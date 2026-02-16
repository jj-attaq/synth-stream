package client

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"

	"github.com/jj-attaq/synth-stream/internal/protocol"
)

type Client struct {
	conn     net.Conn
	username string
	scanner  *bufio.Scanner
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
	}, nil
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
		fmt.Printf("\r%s\n> ", string(packet.Payload))
	}
}

func (c *Client) ChatLoop() {
	fmt.Print("> ")
	for c.scanner.Scan() {
		if err := protocol.WriteMessage(c.conn, protocol.TypeText, c.scanner.Bytes()); err != nil {
			log.Printf("could not send message")
			return
		}
		fmt.Print("> ")
	}
}
