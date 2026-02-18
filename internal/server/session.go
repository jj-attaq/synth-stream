package server

import "errors"

type Session struct {
	ID      string
	Client1 *Client
	Client2 *Client
}

func NewSession(id string, c1, c2 *Client) (*Session, error) {
	session := &Session{
		ID:      id,
		Client1: c1,
		Client2: c2,
	}

	c1.Session = session
	c2.Session = session

	return session, nil
}

func (s *Session) GetPartner(client *Client) (*Client, error) {
	if s.Client1 == nil || s.Client2 == nil {
		return nil, errors.New("partner is nil")
	}
	if client != s.Client1 && client != s.Client2 {
		return nil, errors.New("client is not part of queried session")
	}
	if client == s.Client1 {
		return s.Client2, nil
	}
	return s.Client1, nil
}
