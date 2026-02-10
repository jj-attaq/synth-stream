package server

import (
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	// "time"

	"github.com/google/uuid"
)

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

// Add Session
func (s *Server) addSession(session *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sessions[session.ID] = session
	log.Printf("session %s created\n", session.ID)
}
func (s *Server) addSessionLocked(session *Session) {
	//Same as addSession but s.mu is already locked
	s.sessions[session.ID] = session
	log.Printf("session %s created\n", session.ID)
}

// Remove Session
func (s *Server) removeSession(session *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.sessions, session.ID)
	log.Printf("session %s deleted", session.ID)
}
func (s *Server) removeSessionLocked(session *Session) {
	delete(s.sessions, session.ID)
	log.Printf("session %s deleted", session.ID)
}

// Add Client
func (s *Server) addClient(client *Client) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.clients[client.Username] = client
	log.Printf("%s joined server\n", client.Username)
}
func (s *Server) addClientLocked(client *Client) {
	s.clients[client.Username] = client
	log.Printf("%s joined server\n", client.Username)
}

// Remove Client
func (s *Server) removeClient(username string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.clients, username)
	log.Printf("%s left server\n", username)
}
func (s *Server) removeClientLocked(username string) {
	delete(s.clients, username)
	log.Printf("%s left server\n", username)
}

func (s *Server) routeToPartner(client *Client, message []byte) error {
	// Check if client has a session
	if client.Session != nil {
		// If yes, find the partner (the OTHER client in the session)
		partner, err := client.Session.GetPartner(client)
		if err != nil {
			return err
		}

		// Send message to partner's Conn
		id := append([]byte(client.Username), []byte(": ")...)

		fmtMsg := append(id, message...)
		_, err = partner.Conn.Write(fmtMsg)
		if err != nil {
			// log.Printf("Error sending to %s: %v\n", partner.Username, err)
			return err
		}
		log.Printf("%s: %s", client.Username, message)
	} else {
		// If no session, maybe log "waiting for partner"
		log.Printf("waiting for partner\n")
		return nil
	}

	return nil
}

// conn is expected to be 1 singular connection, because of the go routine in s.Start()
func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	//IDing of user:
	//proper identification of client will need to be created/called here
	//currently no security and a user just puts in their name, there isn't
	//even a prompt
	message, err := readMessage(conn)
	if err != nil {
		fmt.Printf("Error reading from connection: %v\n", err)
		return
	}

	username := strings.TrimSpace(string(message))
	client, err := NewClient(username, conn)
	if err != nil {
		log.Printf("Could not create client for user: %s", username)
		return
	}

	//Create said user:
	s.addClient(client)
	defer s.cleanupClient(client)
	defer s.removeClient(client.Username)

	//pairing logic
	err = s.pairClients(client)
	if err != nil {
		log.Println(err)
	}

	//Conversation loop:
	for {
		message, err := readMessage(conn)
		if err != nil {
			fmt.Printf("Error reading from connection: %v\n", err)
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
		partner.Conn.Write(msg)

		s.removeSessionLocked(client.Session)
		client.Session = nil
		partner.Session = nil
		return
	}
}

func readMessage(conn net.Conn) ([]byte, error) {
	buffer := make([]byte, 1024)
	n, err := conn.Read(buffer)
	if err != nil {
		return nil, err
	}
	return buffer[:n], nil
}
