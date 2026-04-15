package client

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/jj-attaq/synth-stream/internal/protocol"
	"github.com/pion/webrtc/v4"
)

// ChatMessage is the JSON payload for chat messages sent over TCP and WebRTC DataChannel.
// Embedding identity in the payload lets both transports remain format-agnostic.
type ChatMessage struct {
	From string `json:"from"`
	Text string `json:"text"`
}

type Client struct {
	mu          sync.Mutex
	conn        net.Conn
	pc          *webrtc.PeerConnection
	token       string
	username    string
	sessionCode string
	isOfferer   bool
	midiOutput  func([]byte) error
	chatSend    func([]byte) error
	pingCh      chan time.Duration
	sigCh       chan protocol.Packet
	quit        bool
}

func New(token string, address string, tlsConfig *tls.Config) (*Client, error) {
	var conn net.Conn
	var err error
	if tlsConfig != nil {
		conn, err = tls.Dial("tcp", address, tlsConfig)
	} else {
		conn, err = net.Dial("tcp", address)
	}
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

func (c *Client) SetMidiOutput(send func([]byte) error) {
	c.midiOutput = send
}

func (c *Client) SetUsername(username string) {
	c.username = username
}

func (c *Client) Close() {
	c.mu.Lock()
	pc := c.pc
	c.mu.Unlock()
	if pc != nil {
		pc.Close()
	}
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

			if packet.Type != protocol.TypeText {
				continue
			}

			msg := string(packet.Payload)
			fmt.Println(msg)
			if strings.HasPrefix(msg, "paired ") {
				break
			}

			if msg, isError := strings.CutPrefix(msg, "error:"); isError {
				return fmt.Errorf("%s", msg)
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

func (c *Client) IsQuit() bool {
	return c.quit
}

// Quit sends session:leave to the server and closes the connection cleanly.
// Called by both the /quit command and the SIGINT handler.
func (c *Client) Quit() {
	protocol.WriteMessage(c.conn, protocol.TypeText, []byte("session:leave"))
	c.quit = true
	c.Close()
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
			if !c.quit {
				if !errors.Is(err, io.EOF) {
					log.Printf("read error: %v", err)
				}
				fmt.Println("\ndisconnected from server")
			}
			return err
		}

		switch packet.Type {
		case protocol.TypeText:
			var msg ChatMessage
			if err := json.Unmarshal(packet.Payload, &msg); err == nil && msg.From != "" {
				fmt.Printf("\r%s: %s\n> ", msg.From, msg.Text)
			} else {
				fmt.Printf("\r%s\n> ", string(packet.Payload))
			}
		case protocol.TypeMidi:
			if c.midiOutput != nil {
				if err := c.midiOutput(packet.Payload); err != nil {
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
				log.Printf("sigCh full, dropped packet type 0x%02x", packet.Type)
			}
		}
	}
}

// SendMidi sends raw MIDI bytes to the partner over the network.
func (c *Client) SendMidi(data []byte) error {
	return protocol.WriteMessage(c.conn, protocol.TypeMidi, data)
}

// ChatLoop reads lines from stdinCh and sends them to the partner.
// It exits when ctx is cancelled (connection died) or stdinCh is closed.
func (c *Client) ChatLoop(ctx context.Context, stdinCh <-chan string) {
	fmt.Print("\n> ")
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
			if line == "/disconnect" {
				c.Close()
				log.Printf("disconnected")
				return
			}
			if line == "/quit" || line == "/q" || line == "/exit" {
				c.Quit()
				return
			}
			msg := ChatMessage{From: c.username, Text: line}
			marshaledMsg, err := json.Marshal(msg)
			if err != nil {
				log.Printf("failed to marshal message")
				continue
			}
			c.mu.Lock()
			send := c.chatSend
			c.mu.Unlock()
			if send != nil {
				if err := send(marshaledMsg); err != nil {
					log.Printf("WebRTC send failed, retrying via TCP")
					// DataChannel is dead but OnConnectionStateChange may not have fired yet.
					// Clear chatSend now so subsequent messages go via TCP immediately.
					c.mu.Lock()
					c.chatSend = nil
					c.mu.Unlock()
					if err := protocol.WriteMessage(c.conn, protocol.TypeText, marshaledMsg); err != nil {
						log.Printf("could not send message")
						return
					}
				}
			} else {
				if err := protocol.WriteMessage(c.conn, protocol.TypeText, marshaledMsg); err != nil {
					log.Printf("could not send message")
					return
				}
			}
			fmt.Printf("%s: %s\n", c.username, line)
			fmt.Print("> ")
		case <-ctx.Done():
			return
		}
	}
}
