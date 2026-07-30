package ipc

import (
	"encoding/json"
	"fmt"
)

type Client struct{}

func NewClient() *Client {
	return &Client{}
}

func (c *Client) Send(req Request) (*Response, error) {
	conn, err := Dial()
	if err != nil {
		return nil, fmt.Errorf("error connecting to ipc: %v", err)
	}

	defer conn.Close()

	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, fmt.Errorf("error encoding request: %w", err)
	}

	var resp Response

	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return nil, fmt.Errorf("error decoding request: %v", err)
	}

	return &resp, nil
}
