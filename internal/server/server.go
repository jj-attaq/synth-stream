package server

import (
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jj-attaq/synth-stream/internal/protocol"
)

var usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]{4,}$`)

const codeChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
const codeLen = 6

type Server struct {
	clients         map[string]*Client
	sessions        map[string]*Session
	pendingSessions map[string]*Client
	mu              sync.RWMutex
	listener        net.Listener
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

	session, err := NewSession(id.String(), creator, joiner)
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("create session: %w", err)
	}
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

func New(address string) (*Server, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}

	return &Server{
		clients:         make(map[string]*Client),
		sessions:        make(map[string]*Session),
		pendingSessions: make(map[string]*Client),
		listener:        listener,
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

		payload := packet.Payload
		if packet.Type == protocol.TypeText {
			prefix := append([]byte(client.Username), []byte(": ")...)
			payload = append(prefix, payload...)
		}

		if err := protocol.WriteMessage(partner.Conn, packet.Type, payload); err != nil {
			return err
		}
		log.Printf("%s sent %d bytes (type 0x%02x)", client.Username, len(packet.Payload), packet.Type)
	} else {
		log.Printf("waiting for partner\n")
		return nil
	}

	return nil
}

// handleConnection runs in its own goroutine per client, spawned by Start().
func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	// Read username
	message, err := protocol.ReadMessage(conn)
	if err != nil {
		if !errors.Is(err, io.EOF) {
			log.Printf("read error from %s: %v", conn.RemoteAddr(), err)
		}
		return
	}
	if message.Type != protocol.TypeText {
		protocol.WriteMessage(conn, protocol.TypeText, []byte("error:first message must be text"))
		return
	}

	username := strings.TrimSpace(string(message.Payload))

	if errMsg := validateUsername(username); errMsg != "" {
		protocol.WriteMessage(conn, protocol.TypeText, []byte("error:"+errMsg))
		return
	}

	client, err := NewClient(username, conn)
	if err != nil {
		log.Printf("Could not create client for user: %s", username)
		return
	}

	if errMsg := s.registerClient(client); errMsg != "" {
		protocol.WriteMessage(conn, protocol.TypeText, []byte("error:"+errMsg))
		return
	}
	defer s.cleanupClient(client)
	defer s.removeClient(client.Username)

	if err := protocol.WriteMessage(conn, protocol.TypeText, []byte("welcome "+client.Username)); err != nil {
		log.Printf("could not welcome user")
		return
	}

	// Read session command (session:create or session:join:<code>)
	sessionCmd, err := protocol.ReadMessage(conn)
	if err != nil {
		if !errors.Is(err, io.EOF) {
			log.Printf("read error from %s: %v", username, err)
		}
		return
	}
	if sessionCmd.Type != protocol.TypeText {
		protocol.WriteMessage(conn, protocol.TypeText, []byte("error:session command must be text"))
		return
	}

	cmd := strings.TrimSpace(string(sessionCmd.Payload))
	switch {
	case cmd == "session:create":
		sessionCode, err := s.handleSessionCreate(client)
		if err != nil {
			log.Printf("session create error: %v", err)
			return
		}
		select {
		case <-client.pairedCh:
			// partner joined, proceed to message loop
		case <-time.After(10 * time.Minute):
			s.removePendingSession(sessionCode)
			protocol.WriteMessage(conn, protocol.TypeText, []byte("error:session expired"))
			return
		}
	case strings.HasPrefix(cmd, "session:join:"):
		code := strings.TrimPrefix(cmd, "session:join:")
		if err := s.handleSessionJoin(client, code); err != nil {
			log.Printf("session join error: %v", err)
			return
		}
	default:
		protocol.WriteMessage(conn, protocol.TypeText, []byte("error:unknown session command"))
		return
	}

	// Message loop
	for {
		message, err := protocol.ReadMessage(conn)
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

		s.routeToPartner(client, message)
	}
}

func (s *Server) cleanupClient(client *Client) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if client.Session != nil {
		partner, _ := client.Session.GetPartner(client)

		msg := []byte(client.Username + " has been disconnected\n")
		if err := protocol.WriteMessage(partner.Conn, protocol.TypeText, msg); err != nil {
			log.Printf("failed to notify %s of disconnect: %v", partner.Username, err)
		}

		s.removeSessionLocked(client.Session)
		client.Session = nil
		partner.Session = nil
	}
}

// validateUsername checks if a username is valid
// Returns an error message to send back to the client, or "" if valid.
func validateUsername(username string) string {
	if !usernameRegex.MatchString(username) {
		return "username must be at least 4 characters and contain only letters, numbers, underscores, or hyphens"
	}

	return ""
}
