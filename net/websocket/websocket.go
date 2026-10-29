// Package websocket implements WebSocket (RFC 6455): a client, as the
// Minecraft services use for signaling and real-time activity, and a server
// side for tests and tools. A Conn carries text and binary messages,
// answers pings and closes with the close handshake. Nothing a peer sends
// is trusted: frames past the read limit, malformed or misused frames and
// unmasked client frames fail the connection.
package websocket

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Opcodes of the frames.
const (
	opContinuation = 0x0
	opText         = 0x1
	opBinary       = 0x2
	opClose        = 0x8
	opPing         = 0x9
	opPong         = 0xa
)

// Close codes of the close handshake.
const (
	CloseNormal        = 1000
	CloseGoingAway     = 1001
	CloseProtocolError = 1002
	CloseInvalidData   = 1007
	CloseTooLarge      = 1009
)

// DefaultReadLimit bounds a message read unless SetReadLimit changes it.
const DefaultReadLimit = 1 << 20

// guid is the GUID of the opening handshake (RFC 6455 section 1.3).
const guid = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

var (
	// ErrClosed is returned once the connection closed.
	ErrClosed = errors.New("websocket: connection closed")
	// ErrTooLarge is returned for a message past the read limit.
	ErrTooLarge = errors.New("websocket: message too large")
	errProtocol = errors.New("websocket: protocol error")
)

// CloseError is returned by ReadMessage once the peer closed the
// connection with a code.
type CloseError struct {
	Code   int
	Reason string
}

func (e *CloseError) Error() string {
	return fmt.Sprintf("websocket: closed with %d %q", e.Code, e.Reason)
}

// Conn is a WebSocket connection. ReadMessage must be called from one
// goroutine at a time; WriteMessage, Ping and Close may be called from any.
type Conn struct {
	conn   net.Conn
	br     *bufio.Reader
	client bool

	writeMu sync.Mutex
	closed  bool // a close frame was sent; guarded by writeMu

	readLimit int
	// message is the message being reassembled from fragments.
	message   []byte
	messageOp byte
}

// SetReadLimit bounds the size of a message read, fragments together.
func (c *Conn) SetReadLimit(n int) { c.readLimit = n }

// SetReadDeadline sets the deadline of the reads of the connection.
func (c *Conn) SetReadDeadline(t time.Time) error { return c.conn.SetReadDeadline(t) }

// Dial opens a WebSocket connection to a ws or wss URL with the headers
// passed added to the opening handshake. TLS uses cfg, or the defaults if
// nil, with the host of the URL as the server name. The response of the
// server is returned whether or not the handshake succeeded.
func Dial(ctx context.Context, rawURL string, header http.Header, cfg *tls.Config) (*Conn, *http.Response, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, nil, err
	}
	var secure bool
	switch u.Scheme {
	case "wss":
		secure = true
	case "ws":
	default:
		return nil, nil, fmt.Errorf("websocket: scheme %q", u.Scheme)
	}
	host := u.Host
	if u.Port() == "" {
		if secure {
			host = net.JoinHostPort(u.Hostname(), "443")
		} else {
			host = net.JoinHostPort(u.Hostname(), "80")
		}
	}
	var d net.Dialer
	raw, err := d.DialContext(ctx, "tcp", host)
	if err != nil {
		return nil, nil, err
	}
	conn := raw
	if secure {
		c := cfg.Clone()
		if c == nil {
			c = &tls.Config{}
		}
		if c.ServerName == "" {
			c.ServerName = u.Hostname()
		}
		tc := tls.Client(raw, c)
		if err := tc.HandshakeContext(ctx); err != nil {
			_ = raw.Close()
			return nil, nil, err
		}
		conn = tc
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Unix(1, 0)) })
	defer stop()

	key := make([]byte, 16)
	_, _ = rand.Read(key)
	nonce := base64.StdEncoding.EncodeToString(key)
	req := &http.Request{
		Method:     http.MethodGet,
		URL:        u,
		Proto:      "HTTP/1.1",
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     http.Header{},
		Host:       u.Host,
	}
	for k, v := range header {
		req.Header[k] = append([]string(nil), v...)
	}
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Sec-WebSocket-Key", nonce)
	req.Header.Set("Sec-WebSocket-Version", "13")
	if err := req.Write(conn); err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, req)
	if err != nil {
		_ = conn.Close()
		return nil, nil, err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols ||
		!strings.EqualFold(resp.Header.Get("Upgrade"), "websocket") ||
		!headerContains(resp.Header, "Connection", "upgrade") ||
		resp.Header.Get("Sec-WebSocket-Accept") != acceptKey(nonce) {
		// The body of a refusal explains it: it is kept for the caller,
		// bounded in size and in the time it takes.
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body = io.NopCloser(strings.NewReader(string(body)))
		_ = conn.Close()
		return nil, resp, fmt.Errorf("websocket: handshake refused: %s", resp.Status)
	}
	if !stop() {
		// The context ended and broke the deadline of the connection.
		_ = conn.Close()
		return nil, resp, context.Cause(ctx)
	}
	_ = conn.SetDeadline(time.Time{})
	return &Conn{conn: conn, br: br, client: true, readLimit: DefaultReadLimit}, resp, nil
}

// Upgrade takes over the connection of a WebSocket opening handshake and
// returns the server side of the WebSocket.
func Upgrade(w http.ResponseWriter, r *http.Request) (*Conn, error) {
	key := r.Header.Get("Sec-WebSocket-Key")
	if r.Method != http.MethodGet || !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") ||
		!headerContains(r.Header, "Connection", "upgrade") || r.Header.Get("Sec-WebSocket-Version") != "13" {
		http.Error(w, "not a websocket handshake", http.StatusBadRequest)
		return nil, errProtocol
	}
	if raw, err := base64.StdEncoding.DecodeString(key); err != nil || len(raw) != 16 {
		http.Error(w, "bad websocket key", http.StatusBadRequest)
		return nil, errProtocol
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "websocket unsupported", http.StatusInternalServerError)
		return nil, errors.New("websocket: response cannot be hijacked")
	}
	conn, brw, err := hj.Hijack()
	if err != nil {
		return nil, err
	}
	resp := "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + acceptKey(key) + "\r\n\r\n"
	if _, err := io.WriteString(conn, resp); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &Conn{conn: conn, br: brw.Reader, readLimit: DefaultReadLimit}, nil
}

// acceptKey returns the Sec-WebSocket-Accept of a Sec-WebSocket-Key.
func acceptKey(key string) string {
	h := sha1.Sum([]byte(key + guid))
	return base64.StdEncoding.EncodeToString(h[:])
}

// headerContains reports whether a comma separated header holds a token.
func headerContains(h http.Header, name, token string) bool {
	for _, v := range h.Values(name) {
		for _, t := range strings.Split(v, ",") {
			if strings.EqualFold(strings.TrimSpace(t), token) {
				return true
			}
		}
	}
	return false
}

// WriteMessage sends a text or binary message as one frame.
func (c *Conn) WriteMessage(text bool, data []byte) error {
	op := byte(opBinary)
	if text {
		op = opText
	}
	return c.writeFrame(op, data)
}

// Ping sends a ping, which the peer answers with a pong.
func (c *Conn) Ping(data []byte) error {
	if len(data) > 125 {
		return errors.New("websocket: ping payload over 125 bytes")
	}
	return c.writeFrame(opPing, data)
}

// Close sends a close frame, if none was sent yet, and closes the
// connection.
func (c *Conn) Close() error {
	_ = c.closeWith(CloseNormal, "")
	return c.conn.Close()
}

// closeWith sends a close frame with a code and a reason.
func (c *Conn) closeWith(code int, reason string) error {
	payload := make([]byte, 2, 2+len(reason))
	binary.BigEndian.PutUint16(payload, uint16(code))
	payload = append(payload, reason...)
	return c.writeFrame(opClose, payload)
}

func (c *Conn) writeFrame(op byte, data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closed {
		return ErrClosed
	}
	if op == opClose {
		c.closed = true
	}
	header := make([]byte, 0, 14)
	header = append(header, 0x80|op)
	maskBit := byte(0)
	if c.client {
		maskBit = 0x80
	}
	switch n := len(data); {
	case n <= 125:
		header = append(header, maskBit|byte(n))
	case n <= 0xffff:
		header = append(header, maskBit|126)
		header = binary.BigEndian.AppendUint16(header, uint16(n))
	default:
		header = append(header, maskBit|127)
		header = binary.BigEndian.AppendUint64(header, uint64(n))
	}
	frame := header
	if c.client {
		var mask [4]byte
		_, _ = rand.Read(mask[:])
		frame = append(frame, mask[:]...)
		start := len(frame)
		frame = append(frame, data...)
		for i := range data {
			frame[start+i] ^= mask[i&3]
		}
	} else {
		frame = append(frame, data...)
	}
	_, err := c.conn.Write(frame)
	return err
}

// ReadMessage returns the next text or binary message. Pings are answered
// and pongs dropped on the way; a close frame of the peer is answered and
// returned as a *CloseError.
func (c *Conn) ReadMessage() (text bool, data []byte, err error) {
	for {
		fin, op, payload, err := c.readFrame()
		if err != nil {
			return false, nil, err
		}
		switch op {
		case opPing:
			if err := c.writeFrame(opPong, payload); err != nil && !errors.Is(err, ErrClosed) {
				return false, nil, err
			}
			continue
		case opPong:
			continue
		case opClose:
			code, reason := CloseNormal, ""
			switch {
			case len(payload) == 1:
				return false, nil, c.fail(CloseProtocolError)
			case len(payload) >= 2:
				code, reason = int(binary.BigEndian.Uint16(payload)), string(payload[2:])
				if !utf8.ValidString(reason) || !validCloseCode(code) {
					return false, nil, c.fail(CloseProtocolError)
				}
			}
			_ = c.closeWith(code, "")
			_ = c.conn.Close()
			return false, nil, &CloseError{Code: code, Reason: reason}
		case opText, opBinary:
			if c.message != nil {
				// A new message before the last one finished.
				return false, nil, c.fail(CloseProtocolError)
			}
			if fin {
				return c.finish(op, payload)
			}
			c.messageOp, c.message = op, append(make([]byte, 0, len(payload)), payload...)
		case opContinuation:
			if c.message == nil {
				return false, nil, c.fail(CloseProtocolError)
			}
			if len(c.message)+len(payload) > c.readLimit {
				return false, nil, c.fail(CloseTooLarge)
			}
			c.message = append(c.message, payload...)
			if fin {
				msg, op := c.message, c.messageOp
				c.message = nil
				return c.finish(op, msg)
			}
		default:
			return false, nil, c.fail(CloseProtocolError)
		}
	}
}

// finish returns a whole message, failing the connection on text that is
// not UTF-8.
func (c *Conn) finish(op byte, msg []byte) (bool, []byte, error) {
	if op == opText && !utf8.Valid(msg) {
		return false, nil, c.fail(CloseInvalidData)
	}
	return op == opText, msg, nil
}

// fail closes the connection with a code for a frame the peer should not
// have sent and returns the error ReadMessage reports.
func (c *Conn) fail(code int) error {
	_ = c.closeWith(code, "")
	_ = c.conn.Close()
	if code == CloseTooLarge {
		return ErrTooLarge
	}
	return errProtocol
}

// validCloseCode reports whether a peer may send a close code.
func validCloseCode(code int) bool {
	switch {
	case code >= 1000 && code <= 1003, code >= 1007 && code <= 1014:
		return true
	case code >= 3000 && code <= 4999:
		return true
	}
	return false
}

// readFrame reads one frame, unmasked.
func (c *Conn) readFrame() (fin bool, op byte, payload []byte, err error) {
	var h [2]byte
	if _, err := io.ReadFull(c.br, h[:]); err != nil {
		return false, 0, nil, err
	}
	fin, op = h[0]&0x80 != 0, h[0]&0x0f
	if h[0]&0x70 != 0 {
		// No extension was agreed on: the reserved bits stay clear.
		return false, 0, nil, c.fail(CloseProtocolError)
	}
	masked := h[1]&0x80 != 0
	if masked == c.client {
		// Clients mask what they send, servers do not.
		return false, 0, nil, c.fail(CloseProtocolError)
	}
	n := uint64(h[1] & 0x7f)
	switch n {
	case 126:
		var b [2]byte
		if _, err := io.ReadFull(c.br, b[:]); err != nil {
			return false, 0, nil, err
		}
		n = uint64(binary.BigEndian.Uint16(b[:]))
		if n < 126 {
			return false, 0, nil, c.fail(CloseProtocolError)
		}
	case 127:
		var b [8]byte
		if _, err := io.ReadFull(c.br, b[:]); err != nil {
			return false, 0, nil, err
		}
		n = binary.BigEndian.Uint64(b[:])
		if n <= 0xffff || n>>63 != 0 {
			return false, 0, nil, c.fail(CloseProtocolError)
		}
	}
	if op >= opClose && (n > 125 || !fin) {
		// Control frames are short and never fragmented.
		return false, 0, nil, c.fail(CloseProtocolError)
	}
	if n > uint64(c.readLimit) {
		return false, 0, nil, c.fail(CloseTooLarge)
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(c.br, mask[:]); err != nil {
			return false, 0, nil, err
		}
	}
	payload = make([]byte, n)
	if _, err := io.ReadFull(c.br, payload); err != nil {
		return false, 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i&3]
		}
	}
	return fin, op, payload, nil
}
