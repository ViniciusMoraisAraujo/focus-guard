// Package ntp implements a minimal NTP client (RFC 5905) on pure stdlib — a
// single UDP exchange with a public time server to validate the wall clock
// against the real time. The Clock Tamper Protection (Fase 2 do
// features-plan) uses it to confirm a suspected clock jump: a public NTP
// answer is the ground truth the local OS clock cannot forge (a user can set
// the OS date, not the NTP server's).
//
// The client is deliberately minimal: one request, one response, a short
// timeout, no leap-second bookkeeping — everything beyond the epoch offset is
// out of scope.
package ntp

import (
	"encoding/binary"
	"errors"
	"net"
	"time"
)

// DefaultServer is the public NTP pool the daemon queries by default
// (pool.ntp.org round-robins geographically).
const DefaultServer = "pool.ntp.org:123"

// DefaultTimeout bounds a single NTP exchange. Short on purpose: the clock
// check runs on the daemon's boot path and periodically — a stalled server
// must never hold either.
const DefaultTimeout = 3 * time.Second

// ErrTimeout reports that the NTP server did not answer within the budget.
var ErrTimeout = errors.New("ntp: servidor não respondeu dentro do orçamento")

// ntpEpochDelta is the offset between the NTP epoch (1900-01-01) and the Unix
// epoch (1970-01-01) in seconds.
const ntpEpochDelta = 2208988800

// Client is an NTP client bound to one server (host:port). The zero value is
// usable with the default server and timeout; New is the explicit constructor.
type Client struct {
	Server  string
	Timeout time.Duration
	// dial is the UDP dialer — stubbable for tests (a real socket would need
	// a live NTP server). Its concrete type is func(network, addr string)
	// wrapped as a var so tests can swap it.
	dial func(network, addr string) (net.Conn, error)
}

// New returns a client for server ("" → DefaultServer) with timeout (0 →
// DefaultTimeout).
func New(server string, timeout time.Duration) *Client {
	if server == "" {
		server = DefaultServer
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	c := &Client{Server: server, Timeout: timeout}
	c.dial = func(network, addr string) (net.Conn, error) {
		return net.DialTimeout(network, addr, c.Timeout)
	}
	return c
}

// Time queries the NTP server and returns the server's current wall clock as
// a time.Time. On failure it returns the local time plus the error — callers
// must treat the error as "suspicion unresolved", never as confirmation.
func (c *Client) Time() (time.Time, error) {
	srv := c.Server
	if srv == "" {
		srv = DefaultServer
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	dial := c.dial
	if dial == nil {
		dial = func(network, addr string) (net.Conn, error) {
			return net.DialTimeout(network, addr, timeout)
		}
	}

	conn, err := dial("udp", srv)
	if err != nil {
		return time.Now(), err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))

	// RFC 5905 request: LI=0, VN=4, Mode=3 (client), stratum/fields zeroed.
	req := make([]byte, 48)
	req[0] = 0x1b // 00 100 011: LI=0, VN=4, Mode=3

	if _, err := conn.Write(req); err != nil {
		return time.Now(), err
	}

	resp := make([]byte, 48)
	n, err := conn.Read(resp)
	if err != nil {
		return time.Now(), err
	}
	if n < 48 {
		return time.Now(), errors.New("ntp: resposta curta demais")
	}

	// Transmit timestamp is bytes 40..47 (seconds fraction): the instant the
	// server sent the reply, NTP epoch. The network transit time is ignored —
	// a few ms is noise for a clock-tamper check with a 5-minute tolerance.
	secs := binary.BigEndian.Uint32(resp[40:44])
	return time.Unix(int64(secs)-ntpEpochDelta, 0), nil
}

// Offset returns the difference between the NTP server's clock and the local
// clock (server − local). A positive offset means the local clock is behind.
func (c *Client) Offset() (time.Duration, error) {
	srvTime, err := c.Time()
	if err != nil {
		return 0, err
	}
	return srvTime.Sub(time.Now()), nil
}
