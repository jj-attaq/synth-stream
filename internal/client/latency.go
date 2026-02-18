package client

import (
	"encoding/binary"
	"fmt"
	"time"

	"github.com/jj-attaq/synth-stream/internal/protocol"
)

// Ping sends a ping to the server and waits for the echo.
// Returns the round-trip duration.
func (c *Client) Ping() (time.Duration, error) {
	// Encode current time as 8 bytes
	now := time.Now().UnixNano()
	payload := make([]byte, 8)
	binary.BigEndian.PutUint64(payload, uint64(now))

	if err := protocol.WriteMessage(c.conn, protocol.TypePing, payload); err != nil {
		return 0, fmt.Errorf("ping send failed: %w", err)
	}

	select {
	case rtt := <-c.pingCh:
		return rtt, nil
	case <-time.After(5 * time.Second):
		return 0, fmt.Errorf("ping timed out")
	}
}
