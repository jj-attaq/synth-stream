package protocol

import (
	"encoding/binary"
	"errors"
	"net"
)

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
	Type   byte
	Length uint16
}

func (h Header) Marshal() ([]byte, error) {
	buf := make([]byte, 3)
	if _, ok := validTypes[h.Type]; !ok {
		return nil, errors.New("invalid type")
	}

	buf[0] = h.Type
	binary.BigEndian.PutUint16(buf[1:], h.Length)

	return buf, nil
}

func (h Header) Unmarshal(data []byte, v any) error

func (h Header) Read(p []byte) (n int, err error)

func (h Header) Write(p []byte) (n int, err error)

func WriteMessage(conn net.Conn, msgType byte, payload []byte)
