package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jj-attaq/synth-stream/internal/auth"
	"github.com/jj-attaq/synth-stream/internal/protocol"
)

const codeChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
const codeLen = 6

type Server struct {
	clients              map[string]*Client
	sessions             map[string]*Session
	pendingSessions      map[string]*Client
	disconnectedSessions map[string]*Session
	reconnectCancels     map[string]context.CancelFunc
	mu                   sync.RWMutex
	listener             net.Listener
	jwtSecret            string
}

func generateSessionCode() string {
	b := make([]byte, codeLen)
	for i := range b {
		b[i] = codeChars[rand.Intn(len(codeChars))]
	}
	return string(b)
}

func (s *Server) handleSessionCreate(client *Client) (string, error) {
	s.mu.Lock()
	var code string
	for {
		code = generateSessionCode()
		if _, exists := s.pendingSessions[code]; !exists {
			break
		}
	}
	s.pendingSessions[code] = client
	s.mu.Unlock()

	if err := protocol.WriteMessage(client.Conn, protocol.TypeText, []byte("session:created:"+code)); err != nil {
		return "", fmt.Errorf("could not send session code: %w", err)
	}
	log.Printf("%s created session %s\n", client.Username, code)
	return code, nil
}

func (s *Server) handleSessionJoin(joiner *Client, code string) error {
	s.mu.Lock()

	// Reconnect path: if this username has a session parked in disconnectedSessions,
	// restore the session regardless of the code supplied.
	if session, exists := s.disconnectedSessions[joiner.Username]; exists {
		if err := session.ReplaceClient(joiner.Username, joiner); err != nil {
			s.mu.Unlock()
			return fmt.Errorf("ReplaceClient: %w", err)
		}
		delete(s.disconnectedSessions, joiner.Username)
		if cancel, ok := s.reconnectCancels[joiner.Username]; ok {
			cancel()
			delete(s.reconnectCancels, joiner.Username)
		}
		s.mu.Unlock()

		partner, _ := joiner.Session.GetPartner(joiner)
		protocol.WriteMessage(partner.Conn, protocol.TypeText, []byte(joiner.Username+" has reconnected"))
		if err := protocol.WriteMessage(joiner.Conn, protocol.TypeText, []byte("paired "+partner.Username)); err != nil {
			return fmt.Errorf("send paired to rejoiner: %w", err)
		}
		log.Printf("%s reconnected to session", joiner.Username)
		return nil
	}

	// Fresh join path.
	creator, exists := s.pendingSessions[code]
	if !exists {
		s.mu.Unlock()
		protocol.WriteMessage(joiner.Conn, protocol.TypeText, []byte("error:session not found"))
		return fmt.Errorf("session %s not found", code)
	}
	delete(s.pendingSessions, code)

	id, err := uuid.NewUUID()
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("create uuid: %w", err)
	}

	session := NewSession(id.String(), creator, joiner)
	s.addSessionLocked(session)
	s.mu.Unlock()

	if err := protocol.WriteMessage(joiner.Conn, protocol.TypeText, []byte("paired "+creator.Username)); err != nil {
		return fmt.Errorf("send paired to joiner: %w", err)
	}
	if err := protocol.WriteMessage(creator.Conn, protocol.TypeText, []byte("paired "+joiner.Username)); err != nil {
		return fmt.Errorf("send paired to creator: %w", err)
	}

	creator.pairedCh <- struct{}{}
	log.Printf("%s and %s paired in session %s\n", creator.Username, joiner.Username, code)
	return nil
}

func (s *Server) removePendingSession(code string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pendingSessions, code)
	log.Printf("pending session %s expired\n", code)
}

func New(address, jwtSecret, certFile, keyFile string) (*Server, error) {
	var listener net.Listener
	var err error
	if certFile != "" && keyFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("could not load TLS cert: %w", err)
		}
		listener, err = tls.Listen("tcp", address, &tls.Config{Certificates: []tls.Certificate{cert}})
	} else {
		listener, err = net.Listen("tcp", address)
	}
	if err != nil {
		return nil, err
	}

	return &Server{
		clients:              make(map[string]*Client),
		sessions:             make(map[string]*Session),
		pendingSessions:      make(map[string]*Client),
		disconnectedSessions: make(map[string]*Session),
		reconnectCancels:     make(map[string]context.CancelFunc),
		listener:             listener,
		jwtSecret:            jwtSecret,
	}, nil
}

func (s *Server) Start() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			log.Printf("Accept error: %v", err)
			continue
		}
		// New go routine created for each client connection
		go s.handleConnection(conn)
	}
}

func (s *Server) addSessionLocked(session *Session) {
	s.sessions[session.ID] = session
	log.Printf("session %s created\n", session.ID)
}

func (s *Server) removeSessionLocked(session *Session) {
	delete(s.sessions, session.ID)
	log.Printf("session %s deleted", session.ID)
}

func (s *Server) registerClient(client *Client) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, taken := s.clients[client.Username]; taken {
		return "username already taken"
	}

	s.addClientLocked(client)
	return ""
}

func (s *Server) addClientLocked(client *Client) {
	s.clients[client.Username] = client
	log.Printf("%s joined server\n", client.Username)
}

func (s *Server) removeClient(username string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.clients, username)
	log.Printf("%s left server\n", username)
}

func (s *Server) routeToPartner(client *Client, packet protocol.Packet) error {
	// Check if client has a session
	if client.Session != nil {
		// If yes, find the partner (the OTHER client in the session)
		partner, err := client.Session.GetPartner(client)
		if err != nil {
			return err
		}

		if err := protocol.WriteMessage(partner.Conn, packet.Type, packet.Payload); err != nil {
			return err
		}
		log.Printf("%s sent %d bytes (type 0x%02x)", client.Username, len(packet.Payload), packet.Type)
	} else {
		log.Printf("waiting for partner\n")
		return nil
	}

	return nil
}

func (s *Server) registerConnection(conn net.Conn) (*Client, error) {
	message, err := protocol.ReadMessage(conn)
	if err != nil {
		if !errors.Is(err, io.EOF) {
			log.Printf("read error from %s: %v", conn.RemoteAddr(), err)
		}
		return nil, err
	}
	if message.Type != protocol.TypeText {
		protocol.WriteMessage(conn, protocol.TypeText, []byte("error:first message must be text"))
		return nil, fmt.Errorf("first message must be text")
	}

	token := strings.TrimSpace(string(message.Payload))

	username, err := auth.ValidateJWT(token, s.jwtSecret)
	if err != nil {
		protocol.WriteMessage(conn, protocol.TypeText, []byte("error:invalid token"))
		return nil, fmt.Errorf("invalid token: %w", err)
	}

	client := NewClient(username, conn)

	if errMsg := s.registerClient(client); errMsg != "" {
		protocol.WriteMessage(conn, protocol.TypeText, []byte("error:"+errMsg))
		return nil, fmt.Errorf("%s", errMsg)
	}

	if err := protocol.WriteMessage(conn, protocol.TypeText, []byte("welcome "+client.Username)); err != nil {
		log.Printf("Could not welcome user")
		return nil, err
	}
	return client, nil
}

func (s *Server) performSessionSetup(client *Client) error {
	sessionCmd, err := protocol.ReadMessage(client.Conn)
	if err != nil {
		if !errors.Is(err, io.EOF) {
			log.Printf("read error from %s: %v", client.Username, err)
		}
		return err
	}
	if sessionCmd.Type != protocol.TypeText {
		protocol.WriteMessage(client.Conn, protocol.TypeText, []byte("error:session command must be text"))
		return fmt.Errorf("session command must be text")
	}

	cmd := strings.TrimSpace(string(sessionCmd.Payload))
	switch {
	case cmd == "session:create":
		sessionCode, err := s.handleSessionCreate(client)
		if err != nil {
			log.Printf("session create error: %v", err)
			return err
		}
		// NewTimer fires once after 10 minutes (session expiry).
		// NewTicker fires repeatedly every 5 seconds (heartbeat).
		timeout := time.NewTimer(10 * time.Minute)
		defer timeout.Stop()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-client.pairedCh:
				return nil
			case <-timeout.C:
				s.removePendingSession(sessionCode)
				protocol.WriteMessage(client.Conn, protocol.TypeText, []byte("error:session expired"))
				return fmt.Errorf("session expired")
			case <-ticker.C:
				// Writing to a dead TCP socket fails immediately, detecting a dropped
				// creator before the 10-minute timeout would otherwise fire.
				if err := protocol.WriteMessage(client.Conn, protocol.TypePing, nil); err != nil {
					s.removePendingSession(sessionCode)
					return fmt.Errorf("creator disconnected while waiting: %w", err)
				}
			}
		}
	case strings.HasPrefix(cmd, "session:join:"):
		code := strings.TrimPrefix(cmd, "session:join:")
		if err := s.handleSessionJoin(client, code); err != nil {
			log.Printf("session join error: %v", err)
			return err
		}
	default:
		protocol.WriteMessage(client.Conn, protocol.TypeText, []byte("error:unknown session command"))
		return fmt.Errorf("unknown session command: %s", cmd)
	}
	return nil
}

// handleConnection runs in its own goroutine per client, spawned by Start().
// It sequences three phases: handshake, session setup, message loop.
// Reconnect detection is handled inside handleSessionJoin.
func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	// Phase 1: Handshake — validate JWT, register client, send welcome.
	client, err := s.registerConnection(conn)
	if err != nil {
		log.Printf("registerConnection: %v", err)
		return
	}

	defer s.cleanupClient(client)
	defer s.removeClient(client.Username)

	// Phase 2: Session setup — read session:create or session:join:<code>.
	// Fresh joins and reconnects are both handled here.
	if err := s.performSessionSetup(client); err != nil {
		log.Printf("performSessionSetup: %v", err)
		return
	}

	// Phase 3: Message loop — route packets to partner until disconnect.
	for {
		message, err := protocol.ReadMessage(client.Conn)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("read error from %s: %v", client.Username, err)
			}
			return
		}

		if message.Type == protocol.TypePing {
			protocol.WriteMessage(conn, protocol.TypePing, message.Payload)
			continue
		}

		// Intentional quit: tear down cleanly so neither partner lands in
		// disconnectedSessions, preventing a false reconnect next session.
		if message.Type == protocol.TypeText && string(message.Payload) == "session:leave" {
			s.teardownSession(client)
			return
		}

		s.routeToPartner(client, message)
	}
}

// teardownSession handles an intentional quit. It clears both partners'
// session pointers and removes the session entirely, so neither user is
// parked in disconnectedSessions and can start a fresh session immediately.
func (s *Server) teardownSession(client *Client) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if client.Session == nil {
		return
	}

	partner, _ := client.Session.GetPartner(client)
	protocol.WriteMessage(partner.Conn, protocol.TypeText, []byte(client.Username+" has left the session"))

	session := client.Session
	client.Session = nil
	partner.Session = nil
	s.removeSessionLocked(session)
}

func (s *Server) cleanupClient(client *Client) {
	s.mu.Lock()

	if client.Session == nil {
		s.mu.Unlock()
		return
	}

	partner, _ := client.Session.GetPartner(client)

	msg := []byte(client.Username + " has been disconnected, 2 minutes to reconnect.\n")
	if err := protocol.WriteMessage(partner.Conn, protocol.TypeText, msg); err != nil {
		log.Printf("failed to notify %s of disconnect: %v", partner.Username, err)
	}

	// Cancel any stale timer goroutine from a previous disconnect.
	if prevCancel, ok := s.reconnectCancels[client.Username]; ok {
		prevCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.reconnectCancels[client.Username] = cancel

	s.disconnectedSessions[client.Username] = client.Session
	client.Session = nil
	s.mu.Unlock()

	go func() {
		select {
		case <-ctx.Done():
			// Cancelled: client reconnected or disconnected again (new timer owns cleanup).
			return
		case <-time.After(2 * time.Minute):
		}

		s.mu.Lock()
		defer s.mu.Unlock()

		session, stillWaiting := s.disconnectedSessions[client.Username]
		if !stillWaiting {
			return
		}
		// Client never came back — clean up for real.
		protocol.WriteMessage(partner.Conn, protocol.TypeText, []byte(client.Username+" has permanently disconnected"))
		partner.Session = nil
		s.removeSessionLocked(session)
		delete(s.disconnectedSessions, client.Username)
		delete(s.reconnectCancels, client.Username)
	}()
}
