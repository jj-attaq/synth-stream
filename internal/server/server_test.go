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
