package client

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"time"

	"github.com/jj-attaq/synth-stream/internal/protocol"
)

type Client struct {
	conn     net.Conn
	username string
	scanner  *bufio.Scanner
	midiSend func([]byte) error
	pingCh   chan time.Duration
}

func New(username string, address string) (*Client, error) {
	conn, err := net.Dial("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("could not connect: %w", err)
	}

	return &Client{
		conn:     conn,
		username: username,
		scanner:  bufio.NewScanner(os.Stdin),
		pingCh:   make(chan time.Duration, 1),
	}, nil
}

func (c *Client) SetMidiSend(send func([]byte) error) {
	c.midiSend = send
}

func (c *Client) Close() {
	c.conn.Close()
}

func (c *Client) Handshake() error {
	if err := protocol.WriteMessage(c.conn, protocol.TypeText, []byte(c.username)); err != nil {
		return fmt.Errorf("could not send username: %w", err)
	}
	return nil
}

func (c *Client) ReadMessages() {
	for {
		packet, err := protocol.ReadMessage(c.conn)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("read error: %v", err)
			}
			fmt.Println("\ndisconnected from server")
			os.Exit(0)
		}

		switch packet.Type {
		case protocol.TypeText:
			fmt.Printf("\r%s\n> ", string(packet.Payload))
		case protocol.TypeMidi:
			// fmt.Printf("\rReceived MIDI: % x\n> ", packet.Payload)
			if c.midiSend != nil {
				if err := c.midiSend(packet.Payload); err != nil {
					log.Printf("midi playback error: %v", err)
				}
			}
		case protocol.TypePing:
			sent := binary.BigEndian.Uint64(packet.Payload)
			rtt := time.Duration(time.Now().UnixNano() - int64(sent))
			c.pingCh <- rtt
		}
	}
}

// SendMidi sends raw MIDI bytes to the partner over the network.
func (c *Client) SendMidi(data []byte) error {
	if err := protocol.WriteMessage(c.conn, protocol.TypeMidi, data); err != nil {
		return err
	}
	return nil
}

func (c *Client) ChatLoop() {
	fmt.Print("> ")
	for c.scanner.Scan() {
		if string(c.scanner.Bytes()) == "/ping" {
			rtt, err := c.Ping()
			if err != nil {
				fmt.Printf("ping failed: %v\n", err)
			} else {
				fmt.Printf("latency: %v\n", rtt)
			}

			fmt.Print("> ")

			continue
		}
		if err := protocol.WriteMessage(c.conn, protocol.TypeText, c.scanner.Bytes()); err != nil {
			log.Printf("could not send message")
			return
		}
		fmt.Print("> ")
	}
}
