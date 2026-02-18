package server

import "net"

type Client struct {
	Username string
	Conn     net.Conn
	Session  *Session
	pairedCh chan struct{}
}

func NewClient(username string, conn net.Conn) (*Client, error) {
	client := Client{
		Username: username,
		Conn:     conn,
		Session:  nil,
		pairedCh: make(chan struct{}, 1),
	}

	return &client, nil
}
