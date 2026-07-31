package ipc

import (
	"encoding/json"
	"fmt"
	"net"
	"time"
)

type Client struct{}

func NewClient() *Client {
	return &Client{}
}

func (c *Client) Send(req Request) (*Response, error) {
	return c.sendWithDialer(req, Dial, 0)
}

// SendWithTimeout sends a request with a dial and I/O timeout.
// A timeout of 0 disables the deadline (equivalent to Send).
func (c *Client) SendWithTimeout(req Request, timeout time.Duration) (*Response, error) {
	return c.sendWithDialer(req, func() (net.Conn, error) { return DialTimeout(timeout) }, timeout)
}

func (c *Client) sendWithDialer(req Request, dial func() (net.Conn, error), timeout time.Duration) (*Response, error) {
	conn, err := dial()
	if err != nil {
		return nil, fmt.Errorf("error connecting to ipc: %v", err)
	}

	defer conn.Close()

	if timeout > 0 {
		if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
			return nil, fmt.Errorf("error setting deadline: %v", err)
		}
	}

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, fmt.Errorf("error encoding request: %w", err)
	}

	var resp Response

	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, fmt.Errorf("error decoding request: %v", err)
	}

	return &resp, nil
}
