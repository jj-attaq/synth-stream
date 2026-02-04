package main

import (
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
)

type Server struct {
	clients  map[string]net.Conn // username -> connection
	mu       sync.RWMutex
	listener net.Listener
}

func (s *Server) addClient(username string, conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.clients[username] = conn
	log.Printf("%s joined server\n", username)
}

func (s *Server) removeClient(username string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.clients, username)
	log.Printf("%s left server\n", username)
}

func (s *Server) broadcast(from string, message []byte) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for username, conn := range s.clients {
		if username == from {
			continue
		}

		id := append([]byte(from), []byte(": ")...)

		fmtMsg := append(id, message...)
		conn.Write(fmtMsg)
		log.Printf("%s: %s\n", from, message)
	}
}

func (s *Server) handleConnection(conn net.Conn) {
	defer conn.Close()

	message, err := readMessage(conn)
	if err != nil {
		fmt.Printf("Error reading from connection: %v\n", err)
		return
	}

	username := strings.TrimSpace(string(message))

	s.addClient(username, conn)
	defer s.removeClient(username)

	for {
		message, err := readMessage(conn)
		if err != nil {
			fmt.Printf("Error reading from connection: %v\n", err)
			return
		}

		s.broadcast(username, message)
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

func main() {
	listener, err := net.Listen("tcp", ":8080")
	if err != nil {
		panic(err)
	}

	srv := &Server{
		clients:  make(map[string]net.Conn),
		listener: listener,
	}

	for {
		conn, err := listener.Accept()
		if err != nil {
			panic(err)
		}

		go srv.handleConnection(conn)
	}
}
