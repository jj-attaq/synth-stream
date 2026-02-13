package protocol

import (
	"encoding/binary"
	"errors"
	"io"
	"net"
)

// The Protocol expects a 3 byte Header, the first byte determines the type of
// message, the next two bytes annouce payload length, since midi can be of
// variable size

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
		return nil, errors.New("invalid type")
	}

	payloadLen := len(p.Payload)
	totalLen := 3 + payloadLen

	buf := make([]byte, totalLen)
	buf[0] = p.Type

	binary.BigEndian.PutUint16(buf[1:3], uint16(payloadLen))
	copy(buf[3:], p.Payload)

	return buf, nil
}

func Unmarshal(v []byte) (Packet, error) {
	// Minimum header length
	if len(v) < 3 {
		return Packet{}, errors.New("data too short for header")
	}

	// Validate type
	// v[0] is the Type, v[1:3] is the payload length
	msgType := v[0]
	if _, ok := validTypes[msgType]; !ok {
		return Packet{}, errors.New("invalid type")
	}

	payloadLen := binary.BigEndian.Uint16(v[1:3])

	if len(v) < 3+int(payloadLen) {
		return Packet{}, errors.New("data too short for announced payload")
	}

	payload := v[3 : 3+payloadLen]

	return Packet{
		Type:    msgType,
		Payload: payload,
	}, nil
}

func WriteMessage(conn net.Conn, msgType byte, payload []byte) error {
	// 1. Create a packet
	packet := Packet{
		Type:    msgType,
		Payload: payload,
	}
	// 2. Marshal it to bytes
	data, err := packet.Marshal()
	if err != nil {
		return err
	}
	// 3. Write those bytes to conn
	_, err = conn.Write(data)
	if err != nil {
		return err
	}
	// 4. Return any error
	return nil
}

func ReadMessage(conn net.Conn) (Packet, error) {
	// Step 1: Read header (how many bytes?)
	headerBuf := make([]byte, 3)
	_, err := io.ReadFull(conn, headerBuf)
	if err != nil {
		return Packet{}, errors.New("could not read header")
	}
	// Step 2: Parse the length from header
	payloadLen := binary.BigEndian.Uint16(headerBuf[1:3])

	payloadBuf := make([]byte, payloadLen)

	// Step 3: Read the payload (how many bytes?)
	_, err = io.ReadFull(conn, payloadBuf)
	if err != nil {
		return Packet{}, errors.New("failed to read payload")
	}

	// Step 4: Unmarshal everything
	packetBuf := make([]byte, 3+payloadLen)
	copy(packetBuf[:3], headerBuf)
	copy(packetBuf[3:], payloadBuf)

	packet, err := Unmarshal(packetBuf)

	if err != nil {
		return Packet{}, err
	}
	return packet, nil
}
