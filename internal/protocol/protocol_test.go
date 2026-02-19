package protocol

import (
	"net"
	"testing"
)

func TestMarshal_ValidTypes(t *testing.T) {
	tests := []struct {
		name    string
		msgType byte
		payload []byte
	}{
		{"TypeText", TypeText, []byte("hello")},
		{"TypeMidi", TypeMidi, []byte{0x90, 0x3C, 0x7F}},
		{"TypePing", TypePing, []byte{0, 1, 2, 3, 4, 5, 6, 7}},
		{"empty payload", TypeText, []byte{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := Packet{Type: tt.msgType, Payload: tt.payload}
			data, err := p.Marshal()
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			if data[0] != tt.msgType {
				t.Errorf("type byte = 0x%02X, want 0x%02X", data[0], tt.msgType)
			}
			if len(data) != 3+len(tt.payload) {
				t.Errorf("data length = %d, want %d", len(data), 3+len(tt.payload))
			}
		})
	}
}

func TestMarshal_InvalidType(t *testing.T) {
	p := Packet{Type: 0xFF, Payload: []byte("test")}
	_, err := p.Marshal()
	if err == nil {
		t.Error("Marshal() expected error for invalid type, got nil")
	}
}

func TestUnmarshal_ValidPacket(t *testing.T) {
	original := Packet{Type: TypeMidi, Payload: []byte{0x90, 0x3C, 0x7F}}
	data, _ := original.Marshal()

	got, err := Unmarshal(data)
	if err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if got.Type != original.Type {
		t.Errorf("Type = 0x%02X, want 0x%02X", got.Type, original.Type)
	}
	if string(got.Payload) != string(original.Payload) {
		t.Errorf("Payload = %v, want %v", got.Payload, original.Payload)
	}
}

func TestUnmarshal_TooShort(t *testing.T) {
	_, err := Unmarshal([]byte{0x01, 0x00})
	if err == nil {
		t.Error("Unmarshal() expected error for too-short data, got nil")
	}
}

func TestUnmarshal_InvalidType(t *testing.T) {
	_, err := Unmarshal([]byte{0xFF, 0x00, 0x00})
	if err == nil {
		t.Error("Unmarshal() expected error for invalid type, got nil")
	}
}

func TestUnmarshal_LengthMismatch(t *testing.T) {
	// Header declares 5-byte payload but only 2 bytes follow
	data := []byte{TypeText, 0x00, 0x05, 0x68, 0x69}
	_, err := Unmarshal(data)
	if err == nil {
		t.Error("Unmarshal() expected error for length mismatch, got nil")
	}
}

func TestWriteReadMessage_RoundTrip(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	payload := []byte("test")
	go func() {
		err := WriteMessage(server, TypeText, payload)
		if err != nil {
			t.Errorf("WriteMessage() error = %v", err)
		}
	}()

	packet, err := ReadMessage(client)
	if err != nil {
		t.Fatalf("ReadMessage() error = %v", err)
	}
	if packet.Type != TypeText {
		t.Errorf("Type = 0x%02X, want 0x%02X", packet.Type, TypeText)
	}
	if string(packet.Payload) != "test" {
		t.Errorf("Payload = %q, want %q", string(packet.Payload), "test")
	}
}
