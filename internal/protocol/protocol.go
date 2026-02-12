package protocol

import (
	"encoding/binary"
	"errors"
	"net"
)

// The Protocol expecets a 3 byte Header, the first byte determines the type of
// message, the next two bytes annouce payload length, since midi can be of
// variable size

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
	Header
	Payload []byte
}

type Header struct {
	Type byte
}

func (p Packet) Marshal() ([]byte, error) {
	if _, ok := validTypes[p.Header.Type]; !ok {
		return nil, errors.New("invalid type")
	}

	payloadLen := len(p.Payload)
	totalLen := 3 + payloadLen

	buf := make([]byte, totalLen)
	buf[0] = p.Header.Type

	binary.BigEndian.PutUint16(buf[1:3], uint16(payloadLen))
	copy(buf[3:], p.Payload)

	return buf, nil
}

func WriteMessage(conn net.Conn, msgType byte, payload []byte)
