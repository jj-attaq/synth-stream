package client

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"

	"github.com/jj-attaq/synth-stream/internal/protocol"
)

type Client struct {
	conn        net.Conn
	token       string
	sessionCode string
	isOfferer   bool
	midiSend    func([]byte) error
	pingCh      chan time.Duration
	sigCh       chan protocol.Packet
}

func New(token string, address string) (*Client, error) {
	conn, err := net.Dial("tcp", address)
	if err != nil {
		return nil, fmt.Errorf("could not connect: %w", err)
	}

	return &Client{
		conn:   conn,
		token:  token,
		pingCh: make(chan time.Duration, 1),
		sigCh:  make(chan protocol.Packet, 8),
	}, nil
}

func (c *Client) SetMidiSend(send func([]byte) error) {
	c.midiSend = send
}

func (c *Client) Close() {
	c.conn.Close()
}

func (c *Client) Handshake() error {
	if err := protocol.WriteMessage(c.conn, protocol.TypeText, []byte(c.token)); err != nil {
		return fmt.Errorf("could not send token: %w", err)
	}

	packet, err := protocol.ReadMessage(c.conn)
	if err != nil {
		return fmt.Errorf("read handshake response: %w", err)
	}

	msg := string(packet.Payload)
	if msg, isError := strings.CutPrefix(msg, "error:"); isError {
		return fmt.Errorf("%s", msg)
	}
	fmt.Println(msg)

	return nil
}

// SessionSetup handles session negotiation after the handshake and before
// the ReadMessages loop starts. It reads user input from stdinCh.
func (c *Client) SessionSetup(stdinCh <-chan string) error {
	fmt.Print("Create session (c) or join session (j): ")
	choice := <-stdinCh
	switch choice {
	case "c":
		if err := protocol.WriteMessage(c.conn, protocol.TypeText, []byte("session:create")); err != nil {
			return fmt.Errorf("could not create session: %w", err)
		}
		for {
			packet, err := protocol.ReadMessage(c.conn)
			if err != nil {
				return fmt.Errorf("read session response: %w", err)
			}
			msg := string(packet.Payload)
			fmt.Println(msg)
			if strings.HasPrefix(msg, "paired ") {
				break
			}

			if code, found := strings.CutPrefix(msg, "session:created:"); found {
				c.sessionCode = code
			}
		}
		c.isOfferer = true
	case "j":
		fmt.Print("Enter Session ID code: ")
		code := <-stdinCh
		if err := protocol.WriteMessage(c.conn, protocol.TypeText, []byte("session:join:"+code)); err != nil {
			return fmt.Errorf("could not join session: %w", err)
		}

		packet, err := protocol.ReadMessage(c.conn)
		if err != nil {
			return fmt.Errorf("read join response: %w", err)
		}

		msg := string(packet.Payload)
		fmt.Println(msg)

		if msg, isError := strings.CutPrefix(msg, "error:"); isError {
			return fmt.Errorf("%s", msg)
		}
		c.sessionCode = code
	default:
		return fmt.Errorf("invalid choice: must be 'c' or 'j'")
	}

	return nil
}

func (c *Client) SessionCode() string {
	return c.sessionCode
}

func (c *Client) IsOfferer() bool {
	return c.isOfferer
}

func (c *Client) SigCh() <-chan protocol.Packet {
	return c.sigCh
}

// Rejoin sends session:join with the stored code without prompting the user.
// Used when reconnecting within the same process where the code is already known.
func (c *Client) Rejoin(code string) error {
	if err := protocol.WriteMessage(c.conn, protocol.TypeText, []byte("session:join:"+code)); err != nil {
		return fmt.Errorf("rejoin send: %w", err)
	}
	packet, err := protocol.ReadMessage(c.conn)
	if err != nil {
		return fmt.Errorf("rejoin read: %w", err)
	}
	msg := string(packet.Payload)
	fmt.Println(msg)
	if msg, isError := strings.CutPrefix(msg, "error:"); isError {
		return fmt.Errorf("%s", msg)
	}
	c.sessionCode = code
	return nil
}

func (c *Client) ReadMessages() error {
	for {
		packet, err := protocol.ReadMessage(c.conn)
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("read error: %v", err)
			}
			fmt.Println("\ndisconnected from server")
			return err
		}

		switch packet.Type {
		case protocol.TypeText:
			fmt.Printf("\r%s\n> ", string(packet.Payload))
		case protocol.TypeMidi:
			if c.midiSend != nil {
				if err := c.midiSend(packet.Payload); err != nil {
					log.Printf("midi playback error: %v", err)
				}
			}
		case protocol.TypePing:
			sent := binary.BigEndian.Uint64(packet.Payload)
			rtt := time.Duration(time.Now().UnixNano() - int64(sent))
			c.pingCh <- rtt
		case protocol.TypeSignalOffer, protocol.TypeSignalAnswer, protocol.TypeICECandidate:
			select {
			case c.sigCh <- packet:
			default:
			}
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

// ChatLoop reads lines from stdinCh and sends them to the partner.
// It exits when ctx is cancelled (connection died) or stdinCh is closed.
func (c *Client) ChatLoop(ctx context.Context, stdinCh <-chan string) {
	fmt.Print("> ")
	for {
		select {
		case line, ok := <-stdinCh:
			if !ok {
				return
			}
			if line == "/ping" {
				rtt, err := c.Ping()
				if err != nil {
					fmt.Printf("ping failed: %v\n", err)
				} else {
					fmt.Printf("latency: %v\n", rtt)
				}
				fmt.Print("> ")
				continue
			}
			if err := protocol.WriteMessage(c.conn, protocol.TypeText, []byte(line)); err != nil {
				log.Printf("could not send message")
				return
			}
			fmt.Print("> ")
		case <-ctx.Done():
			return
		}
	}
}
