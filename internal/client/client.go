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
	"strings"
	"time"

	"github.com/jj-attaq/synth-stream/internal/protocol"
)

type Client struct {
	conn        net.Conn
	username    string
	sessionCode string
	scanner     *bufio.Scanner
	midiSend    func([]byte) error
	pingCh      chan time.Duration
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
// the ReadMessages loop starts. It does synchronous reads directly from the
// connection since ReadMessages is not yet running.
func (c *Client) SessionSetup() error {
	fmt.Print("Create session (c) or join session (j): ")
	c.scanner.Scan()
	switch string(c.scanner.Bytes()) {
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
	case "j":
		fmt.Print("Enter Session ID code: ")
		c.scanner.Scan()
		code := c.scanner.Text()
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
