package server

import (
	"net"
	"strings"
	"testing"

	"github.com/jj-attaq/synth-stream/internal/protocol"
)

func TestValidateUsername_Valid(t *testing.T) {
	valid := []string{"alice", "bob123", "user_name", "user-name", "abcd", "ALICE"}
	for _, u := range valid {
		t.Run(u, func(t *testing.T) {
			if msg := validateUsername(u); msg != "" {
				t.Errorf("validateUsername(%q) = %q, want empty", u, msg)
			}
		})
	}
}

func TestValidateUsername_TooShort(t *testing.T) {
	short := []string{"", "a", "ab", "abc"}
	for _, u := range short {
		t.Run(u, func(t *testing.T) {
			if msg := validateUsername(u); msg == "" {
				t.Errorf("validateUsername(%q) expected error, got empty", u)
			}
		})
	}
}

func TestValidateUsername_InvalidChars(t *testing.T) {
	invalid := []string{"user name", "user@name", "user!name", "user.name"}
	for _, u := range invalid {
		t.Run(u, func(t *testing.T) {
			if msg := validateUsername(u); msg == "" {
				t.Errorf("validateUsername(%q) expected error, got empty", u)
			}
		})
	}
}

func TestGenerateSessionCode_Length(t *testing.T) {
	for i := 0; i < 100; i++ {
		code := generateSessionCode()
		if len(code) != codeLen {
			t.Errorf("generateSessionCode() length = %d, want %d", len(code), codeLen)
		}
	}
}

func TestGenerateSessionCode_ValidChars(t *testing.T) {
	for i := 0; i < 100; i++ {
		code := generateSessionCode()
		for _, c := range code {
			if !strings.ContainsRune(codeChars, c) {
				t.Errorf("generateSessionCode() contains invalid char %q in %q", c, code)
			}
		}
	}
}

func TestNewClient_PairedChBuffered(t *testing.T) {
	c, err := NewClient("alice", nil)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	// pairedCh must be buffered (cap 1) so a send never blocks the joiner goroutine
	if cap(c.pairedCh) != 1 {
		t.Errorf("pairedCh capacity = %d, want 1", cap(c.pairedCh))
	}
}

func TestRegisterClient_UniqueUsername(t *testing.T) {
	s := &Server{clients: make(map[string]*Client)}
	c := &Client{Username: "alice"}
	if msg := s.registerClient(c); msg != "" {
		t.Errorf("registerClient() first registration failed: %q", msg)
	}
}

func TestRegisterClient_DuplicateUsername(t *testing.T) {
	s := &Server{clients: make(map[string]*Client)}
	c1 := &Client{Username: "alice"}
	c2 := &Client{Username: "alice"}

	s.registerClient(c1)
	if msg := s.registerClient(c2); msg == "" {
		t.Error("registerClient() expected error for duplicate username, got empty")
	}
}

func newTestServer() *Server {
	return &Server{
		clients:           make(map[string]*Client),
		sessions:          make(map[string]*Session),
		pendingSessions:   make(map[string]*Client),
		disconnectedSlots: make(map[string]*Session),
	}
}

func TestRegisterConnection_Valid(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	s := newTestServer()
	result := make(chan *Client, 1)
	go func() {
		c, err := s.registerConnection(serverConn)
		if err != nil {
			t.Errorf("registerConnection() error = %v", err)
		}
		result <- c
	}()

	protocol.WriteMessage(clientConn, protocol.TypeText, []byte("alice"))

	packet, err := protocol.ReadMessage(clientConn)
	if err != nil {
		t.Fatalf("ReadMessage() error = %v", err)
	}
	if string(packet.Payload) != "welcome alice" {
		t.Errorf("payload = %q, want %q", string(packet.Payload), "welcome alice")
	}

	c := <-result
	if c.Username != "alice" {
		t.Errorf("Username = %q, want %q", c.Username, "alice")
	}
}

func TestRegisterConnection_InvalidUsername(t *testing.T) {
	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	s := newTestServer()
	go s.registerConnection(serverConn)

	protocol.WriteMessage(clientConn, protocol.TypeText, []byte("ab"))

	packet, err := protocol.ReadMessage(clientConn)
	if err != nil {
		t.Fatalf("ReadMessage() error = %v", err)
	}
	if !strings.HasPrefix(string(packet.Payload), "error:") {
		t.Errorf("expected error response, got %q", string(packet.Payload))
	}
}

func TestRegisterConnection_DuplicateUsername(t *testing.T) {
	s := newTestServer()
	s.clients["alice"] = &Client{Username: "alice"}

	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	defer clientConn.Close()

	go s.registerConnection(serverConn)

	protocol.WriteMessage(clientConn, protocol.TypeText, []byte("alice"))

	packet, err := protocol.ReadMessage(clientConn)
	if err != nil {
		t.Fatalf("ReadMessage() error = %v", err)
	}
	if !strings.HasPrefix(string(packet.Payload), "error:") {
		t.Errorf("expected error response, got %q", string(packet.Payload))
	}
}

func TestCleanupClient_ParksInDisconnectedSlots(t *testing.T) {
	c1ServerConn, c1ClientConn := net.Pipe()
	c2ServerConn, c2ClientConn := net.Pipe()
	defer c1ServerConn.Close()
	defer c1ClientConn.Close()
	defer c2ServerConn.Close()
	defer c2ClientConn.Close()

	c1 := &Client{Username: "alice", Conn: c1ServerConn}
	c2 := &Client{Username: "bob", Conn: c2ServerConn}
	session, _ := NewSession("test-id", c1, c2)

	s := newTestServer()
	s.sessions[session.ID] = session

	// drain the disconnect notification sent to c2
	go protocol.ReadMessage(c2ClientConn)

	s.cleanupClient(c1)

	if _, exists := s.disconnectedSlots["alice"]; !exists {
		t.Error("expected alice in disconnectedSlots after cleanup")
	}
	if _, exists := s.sessions[session.ID]; !exists {
		t.Error("expected session to remain in sessions map during reconnect window")
	}
	if c1.Session != nil {
		t.Error("expected c1.Session to be nil after cleanup")
	}
	if c2.Session == nil {
		t.Error("expected c2.Session to remain set while partner may reconnect")
	}
}

func TestRouteToPartner_TextPrependsUsername(t *testing.T) {
	c1Conn, p1 := net.Pipe()
	c2Conn, p2 := net.Pipe()
	defer c1Conn.Close()
	defer c2Conn.Close()
	defer p1.Close()
	defer p2.Close()

	c1 := &Client{Username: "alice", Conn: p1}
	c2 := &Client{Username: "bob", Conn: p2}
	NewSession("id", c1, c2)

	s := &Server{}
	done := make(chan protocol.Packet, 1)
	go func() {
		packet, _ := protocol.ReadMessage(c2Conn)
		done <- packet
	}()

	packet := protocol.Packet{Type: protocol.TypeText, Payload: []byte("hello")}
	if err := s.routeToPartner(c1, packet); err != nil {
		t.Fatalf("routeToPartner() error = %v", err)
	}

	received := <-done
	want := "alice: hello"
	if string(received.Payload) != want {
		t.Errorf("payload = %q, want %q", string(received.Payload), want)
	}
}

func TestRouteToPartner_MidiForwardedRaw(t *testing.T) {
	c1Conn, p1 := net.Pipe()
	c2Conn, p2 := net.Pipe()
	defer c1Conn.Close()
	defer c2Conn.Close()
	defer p1.Close()
	defer p2.Close()

	c1 := &Client{Username: "alice", Conn: p1}
	c2 := &Client{Username: "bob", Conn: p2}
	NewSession("id", c1, c2)

	s := &Server{}
	midiBytes := []byte{0x90, 0x3C, 0x7F}
	done := make(chan protocol.Packet, 1)
	go func() {
		packet, _ := protocol.ReadMessage(c2Conn)
		done <- packet
	}()

	packet := protocol.Packet{Type: protocol.TypeMidi, Payload: midiBytes}
	if err := s.routeToPartner(c1, packet); err != nil {
		t.Fatalf("routeToPartner() error = %v", err)
	}

	received := <-done
	if string(received.Payload) != string(midiBytes) {
		t.Errorf("MIDI payload modified: got %v, want %v", received.Payload, midiBytes)
	}
}
