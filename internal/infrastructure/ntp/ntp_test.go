package ntp

import (
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"
)

// fakeUDPConn é um net.Conn de teste que responde a cada Read com o conteúdo
// pré-codificado (um pacote NTP com o transmit timestamp desejado).
type fakeUDPConn struct {
	written []byte
	reply   []byte
}

func (f *fakeUDPConn) Read(b []byte) (int, error) {
	if len(f.reply) > len(b) {
		return 0, errTooSmall
	}
	copy(b, f.reply)
	return len(f.reply), nil
}

func (f *fakeUDPConn) Write(b []byte) (int, error) {
	f.written = append(f.written, b...)
	return len(b), nil
}

func (f *fakeUDPConn) Close() error                       { return nil }
func (f *fakeUDPConn) LocalAddr() net.Addr                { return &net.UDPAddr{} }
func (f *fakeUDPConn) RemoteAddr() net.Addr               { return &net.UDPAddr{} }
func (f *fakeUDPConn) SetDeadline(t time.Time) error      { return nil }
func (f *fakeUDPConn) SetReadDeadline(t time.Time) error  { return nil }
func (f *fakeUDPConn) SetWriteDeadline(t time.Time) error { return nil }

type fakeErr struct{ msg string }

func (e *fakeErr) Error() string { return e.msg }

var errTooSmall = &fakeErr{"fake: buffer pequeno"}

// ntpPacket builds a 48-byte NTP response with the given transmit timestamp
// (Unix seconds) and a fixed LI/VN/Mode byte.
func ntpPacket(transmitUnix int64) []byte {
	p := make([]byte, 48)
	p[0] = 0x1c // LI=0, VN=4, Mode=4 (server)
	binary.BigEndian.PutUint32(p[40:44], uint32(transmitUnix+ntpEpochDelta))
	return p
}

// clientWithFake dial returns a Client whose dial returns the fake conn.
func clientWithFake(reply []byte) (*Client, *fakeUDPConn) {
	fake := &fakeUDPConn{reply: reply}
	c := New("ntp.example:123", time.Second)
	c.dial = func(_, _ string) (net.Conn, error) { return fake, nil }
	return c, fake
}

func TestTimeReturnsServerClock(t *testing.T) {
	want := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	c, fake := clientWithFake(ntpPacket(want.Unix()))

	got, err := c.Time()
	if err != nil {
		t.Fatal(err)
	}
	if got.Unix() != want.Unix() {
		t.Errorf("Time = %v, want %v", got, want)
	}
	if len(fake.written) != 48 || fake.written[0] != 0x1b {
		t.Errorf("request malformado: len=%d first=0x%02x, want 48 bytes com 0x1b", len(fake.written), fake.written[0])
	}
}

func TestShortResponseFails(t *testing.T) {
	c, _ := clientWithFake([]byte{1, 2, 3})
	if _, err := c.Time(); err == nil {
		t.Fatal("resposta curta deveria falhar")
	}
}

func TestDialErrorSurfaces(t *testing.T) {
	c := New("", time.Second)
	c.dial = func(_, _ string) (net.Conn, error) { return nil, &fakeErr{"connection refused"} }

	if _, err := c.Time(); err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("erro de dial deveria subir, got %v", err)
	}
}

func TestReadErrorSurfaces(t *testing.T) {
	c := New("", time.Second)
	c.dial = func(_, _ string) (net.Conn, error) { return &errorConn{err: &fakeErr{"read timeout"}}, nil }
	if _, err := c.Time(); err == nil || !strings.Contains(err.Error(), "read timeout") {
		t.Errorf("erro de read deveria subir, got %v", err)
	}
}

type errorConn struct {
	err error
}

func (e *errorConn) Read(b []byte) (int, error)         { return 0, e.err }
func (e *errorConn) Write(b []byte) (int, error)        { return 0, nil }
func (e *errorConn) Close() error                       { return nil }
func (e *errorConn) LocalAddr() net.Addr                { return &net.UDPAddr{} }
func (e *errorConn) RemoteAddr() net.Addr               { return &net.UDPAddr{} }
func (e *errorConn) SetDeadline(t time.Time) error      { return nil }
func (e *errorConn) SetReadDeadline(t time.Time) error  { return nil }
func (e *errorConn) SetWriteDeadline(t time.Time) error { return nil }

func TestOffsetSign(t *testing.T) {
	// Servidor 1h à frente do relógio local → offset positivo.
	server := time.Now().Add(time.Hour)
	c, _ := clientWithFake(ntpPacket(server.Unix()))

	off, err := c.Offset()
	if err != nil {
		t.Fatal(err)
	}
	if off < 30*time.Minute {
		t.Errorf("Offset = %v, want ~1h positivo (local atrás do servidor)", off)
	}
}

func TestDefaultServerAndTimeout(t *testing.T) {
	c := New("", 0)
	if c.Server != DefaultServer {
		t.Errorf("Server = %q, want default %q", c.Server, DefaultServer)
	}
	if c.Timeout != DefaultTimeout {
		t.Errorf("Timeout = %v, want %v", c.Timeout, DefaultTimeout)
	}
}
