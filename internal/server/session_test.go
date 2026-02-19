package server

import (
	"testing"
)

func TestNewSession(t *testing.T) {
	c1 := &Client{Username: "alice"}
	c2 := &Client{Username: "bob"}

	session, err := NewSession("test-id", c1, c2)
	if err != nil {
		t.Fatalf("NewSession() error = %v", err)
	}
	if session.ID != "test-id" {
		t.Errorf("ID = %q, want %q", session.ID, "test-id")
	}
	if c1.Session != session {
		t.Error("c1.Session not set correctly")
	}
	if c2.Session != session {
		t.Error("c2.Session not set correctly")
	}
}

func TestGetPartner_Client1GetsClient2(t *testing.T) {
	c1 := &Client{Username: "alice"}
	c2 := &Client{Username: "bob"}
	session, _ := NewSession("id", c1, c2)

	partner, err := session.GetPartner(c1)
	if err != nil {
		t.Fatalf("GetPartner() error = %v", err)
	}
	if partner != c2 {
		t.Errorf("GetPartner(c1) = %q, want %q", partner.Username, c2.Username)
	}
}

func TestGetPartner_Client2GetsClient1(t *testing.T) {
	c1 := &Client{Username: "alice"}
	c2 := &Client{Username: "bob"}
	session, _ := NewSession("id", c1, c2)

	partner, err := session.GetPartner(c2)
	if err != nil {
		t.Fatalf("GetPartner() error = %v", err)
	}
	if partner != c1 {
		t.Errorf("GetPartner(c2) = %q, want %q", partner.Username, c1.Username)
	}
}

func TestGetPartner_NonMember(t *testing.T) {
	c1 := &Client{Username: "alice"}
	c2 := &Client{Username: "bob"}
	stranger := &Client{Username: "eve"}
	session, _ := NewSession("id", c1, c2)

	_, err := session.GetPartner(stranger)
	if err == nil {
		t.Error("GetPartner() expected error for non-member, got nil")
	}
}

func TestGetPartner_NilClients(t *testing.T) {
	session := &Session{ID: "id", Client1: nil, Client2: nil}
	c := &Client{Username: "alice"}

	_, err := session.GetPartner(c)
	if err == nil {
		t.Error("GetPartner() expected error when clients are nil, got nil")
	}
}
