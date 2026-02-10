package protocol

import (
	"io"
	"net"
)

type Packet struct {
	Header
	Payload []byte
}

type Header struct {
	Type   [1]byte
	Length [2]byte
}

func WriteMessage(conn net.Conn, msgType byte, payload []byte) {
	io.
}

// func WriteMessage(conn net.Conn, msgType) {
// 	buf := append([]byte, header.Type, header.Length)
// 	io.ReadFull(conn, buf)
// }
