package server

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"regexp"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/jj-attaq/synth-stream/internal/protocol"
)

var usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]{4,}$`)

type Server struct {
	clients       map[string]*Client
	sessions      map[string]*Session
	waitingClient *Client
	mu            sync.RWMutex
	listener      net.Listener
}

func (s *Server) pairClients(client *Client) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.waitingClient == nil {
		s.waitingClient = client
		if err := protocol.WriteMessage(s.waitingClient.Conn, protocol.TypeText, []byte("waiting...")); err != nil {
			return fmt.Errorf("could not send waiting message")
		}

		log.Printf("%s is waiting to be paired\n", s.waitingClient.Username)
		return nil
	}

	id, err := uuid.NewUUID()
	if err != nil {
		return fmt.Errorf("uuid could not be created")
	}

	session, err := NewSession(id.String(), s.waitingClient, client)
	if err != nil {
		return fmt.Errorf("new session not created")
	}

	if err := protocol.WriteMessage(client.Conn, protocol.TypeText, []byte("paired "+s.waitingClient.Username)); err != nil {
		return fmt.Errorf("could not send pairing message")
	}

	if err := protocol.WriteMessage(s.waitingClient.Conn, protocol.TypeText, []byte("paired "+client.Username)); err != nil {
		return fmt.Errorf("could not send pairing message")
	}

	s.waitingClient = nil

	s.addSessionLocked(session)
	log.Printf("%s and %s paired\n", session.Client1.Username, session.Client2.Username)

	return nil
}

func New(address string) (*Server, error) {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return nil, err
	}

	return &Server{
		clients:  make(map[string]*Client),
		sessions: make(map[string]*Session),
		listener: listener,
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

// conn is expected to be 1 singular connection, because of the go routine in s.Start()
func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

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

	//pairing logic
	err = s.pairClients(client)
	if err != nil {
		log.Println(err)
	}

	//Conversation loop:
	for {
		message, err := protocol.ReadMessage(conn)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("read error from %s: %v", client.Username, err)
			}
			return
		}

		s.routeToPartner(client, message)
	}
}

func (s *Server) cleanupClient(client *Client) { //The client that has been disconnected
	s.mu.Lock()
	defer s.mu.Unlock()

	// If they're the waiting client, clear it
	if client == s.waitingClient {
		s.waitingClient = nil
		return
	}
	// If they have a session, handle it
	if client.Session != nil {
		partner, _ := client.Session.GetPartner(client)

		msg := append([]byte(client.Username), []byte(" has been disconnected\n")...)
		if err := protocol.WriteMessage(partner.Conn, protocol.TypeText, msg); err != nil {
			log.Printf("failed to notify %s of disconnect: %v", partner.Username, err)
		}

		s.removeSessionLocked(client.Session)
		client.Session = nil
		partner.Session = nil
		return
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
