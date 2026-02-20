package server

import "fmt"

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
		return nil, fmt.Errorf("partner is nil")
	}
	if client != s.Client1 && client != s.Client2 {
		return nil, fmt.Errorf("client is not part of queried session")
	}
	if client == s.Client1 {
		return s.Client2, nil
	}
	return s.Client1, nil
}

func (s *Session) ReplaceClient(username string, newClient *Client) error {
	switch username {
	case s.Client1.Username:
		s.Client1 = newClient
		newClient.Session = s
	case s.Client2.Username:
		s.Client2 = newClient
		newClient.Session = s
	default:
		return fmt.Errorf("client %s not in session", username)
	}
	return nil
}
