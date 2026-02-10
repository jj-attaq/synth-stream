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
	defer s.removeClient(client.Username)

	//pairing logic
	err = s.pairClients(client)
	if err != nil {
		log.Println(err)
	}

	//Conversation loop:
	for {
		// session := client.Session
		message, err := readMessage(conn)
		if err != nil {
			fmt.Printf("Error reading from connection: %v\n", err)
			return
		}

		s.routeToPartner(client, message)

		//Delete session if both disconnect.
		// if session.Client1 == nil && session.Client2 == nil {
		// 	s.removeSession(client.Session)
		// 	log.Printf("session: %s for users: %s, %s removed\n", session.ID, session.Client1.Username, session.Client2.Username)
		// }

		// //One client has disconnected from the session
		// if session.Client1 == nil || session.Client2 == nil {
		// 	var remainingClient *Client
		// 	var disconnectedClient *Client
		// 	if client == session.Client1 {
		// 		log.Printf("%s has been disconnected from the session\n", session.Client2.Username)
		// 		remainingClient = session.Client2
		// 		disconnectedClient = session.Client1
		// 	}
		// 	log.Printf("%s has been disconnected from the session\n", session.Client1.Username)
		// 	remainingClient = session.Client1
		// 	disconnectedClient = session.Client2
		//
		// 	//Need to add better connection experience at start, usernames associated with session IDs
		// 	log.Printf("Would you like to disconnect as well y/n?\n")
		// 	confirmation, err := readMessage(conn)
		// 	if err != nil {
		// 		fmt.Printf("Error reading from connection: %v\n", err)
		// 		return
		// 	}
		//
		// 	if string(confirmation) == "y" {
		// 		s.removeClient(remainingClient.Username)
		// 		s.removeSession(remainingClient.Session)
		// 		return
		// 	} else {
		// 		log.Printf("Session will remain active until %s reconnects to session or until timeout\n", disconnectedClient.Username)
		// 		timer := time.NewTimer(5 * time.Second)
		// 		<-timer.C // Wait for the timer to fire
		// 		if session.Client1 != nil && session.Client2 != nil {
		// 			continue
		// 		}
		//
		// 		s.removeClient(remainingClient.Username)
		// 		s.removeSession(remainingClient.Session)
		// 	}
		// }
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
