package server

import "net"

type Client struct {
	Username string
	Conn     net.Conn
	Session  *Session
}

func NewClient(username string, conn net.Conn) (*Client, error) {
	client := Client{
		Username: username,
		Conn:     conn,
		Session:  nil,
	}

	return &client, nil
}
