package main

import (
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"

	"github.com/google/uuid"
)

type session struct {
	ID           string
	Participants []string
	Events       []midiEvent
	Listener     net.Listener
	mu           sync.RWMutex
}

func (s *session) addParticipant(p string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Participants = append(s.Participants, p)
	fmt.Printf("Number of participants: %d\n", len(s.Participants))
}

var users = []string{"A", "B"}
var eventTypes = []string{"note_on", "note_off"}

type midiEvent struct {
	Type string
	User string
	Note int
}

func newMidiEvent(t, u string, n int) (midiEvent, error) {
	if !slices.Contains(eventTypes, t) {
		return midiEvent{}, fmt.Errorf("Not a valid event type")
	}

	if !slices.Contains(users, u) {
		return midiEvent{}, fmt.Errorf("Not a valid user")
	}

	return midiEvent{
		Type: t,
		User: u,
		Note: n,
	}, nil
}

func newSession(addr string) (session, error) {
	newUUID := uuid.New()
	id := newUUID.String()

	ln, err := net.Listen("tcp", ":"+addr)
	if err != nil {
		panic(err)
	}

	return session{
		ID:       id,
		Listener: ln,
	}, nil
}

func main() {
	// e, err := newMidiEvent(eventTypes[0], users[0], 60)
	// if err != nil {
	// 	panic(err)
	// }
	// fmt.Println(e)

	// TCP Server
	s, err := newSession("8080")
	if err != nil {
		panic(err)
	}

	for {
		conn, err := s.Listener.Accept()
		if err != nil {
			panic(err)
		}

		go handleConnection(conn, &s)
	}
}

// echo tcp
func handleConnection(conn net.Conn, s *session) {
	defer conn.Close()

	// username or identifier
	buffer := make([]byte, 1024)
	n, err := conn.Read(buffer)
	if err != nil {
		fmt.Println("Error reading:", err)
		return
	}

	// parse username
	userName := string(buffer[:n])
	userName = strings.TrimSpace(userName)

	// add
	s.addParticipant(userName)

	// echo back
	_, err = conn.Write(buffer[:n])
	if err != nil {
		fmt.Println("Error writing:", err)
	}
}
