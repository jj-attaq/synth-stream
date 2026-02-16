package protocol

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
)

// Protocol wire format:
//   Byte 0:    Message type (TypeText, TypeMidi, TypeAudio)
//   Bytes 1-2: Payload length (uint16, big-endian)
//   Bytes 3+:  Payload data

const (
	TypeText  = 0x01
	TypeMidi  = 0x10
	TypeAudio = 0x20
)

var validTypes = map[byte]struct{}{
	TypeText:  {},
	TypeMidi:  {},
	TypeAudio: {},
}

type Packet struct {
	Type    byte
	Payload []byte
}

func (p Packet) Marshal() ([]byte, error) {
	if _, ok := validTypes[p.Type]; !ok {
		return nil, fmt.Errorf("invalid message type: 0x%02X", p.Type)
	}

	payloadLen := len(p.Payload)
	buf := make([]byte, 3+payloadLen)

	buf[0] = p.Type
	binary.BigEndian.PutUint16(buf[1:3], uint16(payloadLen))
	copy(buf[3:], p.Payload)

	return buf, nil
}

func Unmarshal(data []byte) (Packet, error) {
	if len(data) < 3 {
		return Packet{}, fmt.Errorf("data too short for header")
	}

	msgType := data[0]
	if _, ok := validTypes[msgType]; !ok {
		return Packet{}, fmt.Errorf("invalid message type: 0x%02X", msgType)
	}

	payloadLen := binary.BigEndian.Uint16(data[1:3])
	if len(data) < 3+int(payloadLen) {
		return Packet{}, fmt.Errorf("data too short: expected %d bytes, got %d", 3+payloadLen, len(data))
	}

	return Packet{
		Type:    msgType,
		Payload: data[3 : 3+payloadLen],
	}, nil
}

func WriteMessage(conn net.Conn, msgType byte, payload []byte) error {
	packet := Packet{Type: msgType, Payload: payload}
	data, err := packet.Marshal()
	if err != nil {
		return fmt.Errorf("marshal failed: %w", err)
	}

	if _, err := conn.Write(data); err != nil {
		return fmt.Errorf("write failed: %w", err)
	}

	return nil
}

func ReadMessage(conn net.Conn) (Packet, error) {
	// Read 3-byte header
	header := make([]byte, 3)
	if _, err := io.ReadFull(conn, header); err != nil {
		return Packet{}, fmt.Errorf("read header failed: %w", err)
	}

	// Parse payload length and read payload
	payloadLen := binary.BigEndian.Uint16(header[1:3])
	payload := make([]byte, payloadLen)
	if _, err := io.ReadFull(conn, payload); err != nil {
		return Packet{}, fmt.Errorf("read payload failed: %w", err)
	}

	// Combine and unmarshal
	fullPacket := make([]byte, 3+payloadLen)
	copy(fullPacket[:3], header)
	copy(fullPacket[3:], payload)

	packet, err := Unmarshal(fullPacket)
	if err != nil {
		return Packet{}, fmt.Errorf("unmarshal failed: %w", err)
	}

	return packet, nil
}
