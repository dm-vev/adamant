// Package dtls implements DTLS 1.2 (RFC 6347) as WebRTC uses it: ECDHE
// with ECDSA certificates checked by fingerprint, AES-GCM records, the
// extended master secret of RFC 7627, both the client and the server role.
//
// A Conn is a state machine without goroutines or I/O: the owner feeds it
// received datagrams and timer expiries with the current time, and gets
// datagrams to send through a callback. It is not safe for concurrent use.
package dtls

import (
	"bytes"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"time"
)

// Config configures a Conn.
type Config struct {
	// Certificate is the certificate the Conn authenticates with.
	Certificate Certificate
	// VerifyPeer checks the certificate chain of the peer, leaf first.
	// WebRTC compares the fingerprint of the leaf with the one the signaling
	// carried. The handshake fails if it returns an error, or if it is nil.
	VerifyPeer func(chain [][]byte) error
	// MTU is the largest datagram the Conn sends. Zero means 1200.
	MTU int
	// HelloVerify makes a server answer the first ClientHello with a cookie
	// the client must echo, proving it receives at its address. It costs a
	// round trip; peers whose address ICE checked do not need it.
	HelloVerify bool
	// HandshakeTimeout bounds the handshake. Zero means 30 seconds.
	HandshakeTimeout time.Duration
}

// Alert descriptions.
const (
	alertCloseNotify       = 0
	alertUnexpectedMessage = 10
	alertHandshakeFailure  = 40
	alertBadCertificate    = 42
	alertIllegalParameter  = 47
	alertDecodeError       = 50
	alertDecryptError      = 51
	alertProtocolVersion   = 70
	alertInternalError     = 80
	alertNoRenegotiation   = 100
)

// AlertError is the error of a Conn that received or sent a fatal alert.
type AlertError struct {
	Description byte
	// Remote is set if the peer sent the alert.
	Remote bool
}

func (e *AlertError) Error() string {
	if e.Remote {
		return fmt.Sprintf("dtls: peer sent alert %d", e.Description)
	}
	return fmt.Sprintf("dtls: sent alert %d", e.Description)
}

// ErrClosed is returned once the Conn is closed, by either side.
var ErrClosed = errors.New("dtls: connection closed")

var errTimeout = errors.New("dtls: handshake timed out")

type state int

const (
	stateStart state = iota
	stateWaitServerHello
	stateWaitServerHelloDone
	stateWaitServerFinished
	stateWaitClientHello
	stateWaitClientFinished
	stateEstablished
	stateFailed
)

const (
	initialRetransmit = time.Second
	maxRetransmit     = 60 * time.Second
	// maxQueued bounds the records of the next epoch and the application
	// data held until the handshake reaches them.
	maxQueued = 16
	// reorderWindow bounds how far ahead of the next message a fragment is
	// kept.
	reorderWindow = 8
)

// flightMessage is a message of the last flight sent, kept to send again.
type flightMessage struct {
	epoch uint16
	ccs   bool
	typ   byte
	seq   uint16
	body  []byte
}

// partial is a handshake message being reassembled from fragments.
type partial struct {
	seq    uint16
	typ    byte
	epoch  uint16
	length int
	body   []byte
	// have holds the received ranges, merged, as start and end pairs.
	have []int
}

func (p *partial) add(start, end int) {
	if start == end {
		return
	}
	p.have = append(p.have, start, end)
	// Merge: the lists are short, keep them sorted by start.
	for i := len(p.have) - 2; i >= 2 && p.have[i] < p.have[i-2]; i -= 2 {
		p.have[i], p.have[i-2] = p.have[i-2], p.have[i]
		p.have[i+1], p.have[i-1] = p.have[i-1], p.have[i+1]
	}
	merged := p.have[:2]
	for i := 2; i < len(p.have); i += 2 {
		last := len(merged) - 1
		if p.have[i] <= merged[last] {
			merged[last] = max(merged[last], p.have[i+1])
			continue
		}
		merged = append(merged, p.have[i], p.have[i+1])
	}
	p.have = merged
}

func (p *partial) complete() bool {
	return p.length == 0 || len(p.have) == 2 && p.have[0] == 0 && p.have[1] == p.length
}

// Conn is one end of a DTLS association.
type Conn struct {
	cfg    Config
	client bool
	send   func([]byte)
	mtu    int
	state  state
	err    error

	// Handshake.
	msgSeqSend, msgSeqRecv uint16
	partials               []*partial
	transcript             []byte
	hello                  *clientHello
	clientRandom           [32]byte
	serverRandom           [32]byte
	suite                  *cipherSuite
	ecdhKey                *ecdh.PrivateKey
	premaster              []byte
	peerChain              [][]byte
	peerKey                any
	certRequested          bool
	requestedSignatures    []uint16
	extendedMaster         bool
	master                 [48]byte
	// verifyLen is the length of the transcript a CertificateVerify signs.
	verifyLen   int
	cookieKey   [32]byte
	flight      []flightMessage
	retransmit  time.Time
	interval    time.Duration
	lastResend  time.Time
	deadline    time.Time
	ccsReceived bool

	// Records.
	writeSeq     [2]uint64
	readEpoch    uint16
	writeCipher  *cipherState
	readCipher   *cipherState
	pendingRead  *cipherState
	replay       replayWindow
	queued       [][]byte
	queuedAppDat [][]byte
	out          []byte
	// deliver is the callback of the HandleDatagram call in progress.
	deliver func([]byte)
}

// Client returns a Conn in the client role. Start sends its first flight.
func Client(cfg Config, send func([]byte)) *Conn {
	return newConn(cfg, true, send)
}

// Server returns a Conn in the server role, waiting for a ClientHello.
func Server(cfg Config, send func([]byte)) *Conn {
	c := newConn(cfg, false, send)
	c.state = stateWaitClientHello
	_, _ = rand.Read(c.cookieKey[:])
	return c
}

func newConn(cfg Config, client bool, send func([]byte)) *Conn {
	c := &Conn{cfg: cfg, client: client, send: send, mtu: cfg.MTU}
	if c.mtu == 0 {
		c.mtu = 1200
	}
	if c.cfg.HandshakeTimeout == 0 {
		c.cfg.HandshakeTimeout = 30 * time.Second
	}
	return c
}

// Start begins the handshake: a client sends its ClientHello, a server
// starts the handshake timeout.
func (c *Conn) Start(now time.Time) {
	c.deadline = now.Add(c.cfg.HandshakeTimeout)
	if !c.client || c.state != stateStart {
		return
	}
	_, _ = rand.Read(c.clientRandom[:])
	suites := make([]uint16, len(cipherSuites))
	for i, s := range cipherSuites {
		suites[i] = s.id
	}
	c.hello = &clientHello{
		version:      version12,
		random:       c.clientRandom,
		cipherSuites: suites,
		compression:  []byte{0},
		ext:          extensions{groups: supportedGroups, signatures: supportedSignatures},
	}
	c.newFlight()
	c.queueHandshake(0, typeClientHello, c.hello.marshal())
	c.state = stateWaitServerHello
	c.sendFlight(now, true)
}

// Established reports whether the handshake completed.
func (c *Conn) Established() bool { return c.state == stateEstablished }

// Err returns the error the Conn failed with, if any.
func (c *Conn) Err() error { return c.err }

// PeerChain returns the certificate chain of the peer once known.
func (c *Conn) PeerChain() [][]byte { return c.peerChain }

// Deadline returns when HandleTimeout must next be called, or the zero time.
func (c *Conn) Deadline() time.Time {
	if c.state == stateEstablished || c.state == stateFailed {
		return time.Time{}
	}
	switch {
	case c.retransmit.IsZero():
		return c.deadline
	case c.deadline.IsZero() || c.retransmit.Before(c.deadline):
		return c.retransmit
	}
	return c.deadline
}

// HandleTimeout sends the last flight again if its retransmission timer
// expired, and fails the handshake past its timeout.
func (c *Conn) HandleTimeout(now time.Time) error {
	if c.err != nil {
		return c.err
	}
	if c.state == stateEstablished {
		return nil
	}
	if !c.deadline.IsZero() && !now.Before(c.deadline) {
		return c.fail(errTimeout)
	}
	if !c.retransmit.IsZero() && !now.Before(c.retransmit) {
		c.interval = min(2*c.interval, maxRetransmit)
		c.sendFlight(now, false)
		c.retransmit = now.Add(c.interval)
	}
	return nil
}

// HandleDatagram processes the records of a received datagram, decrypting
// in place. deliver is called with each application data record; its
// argument is valid until it returns.
func (c *Conn) HandleDatagram(b []byte, now time.Time, deliver func([]byte)) error {
	if c.err != nil {
		return c.err
	}
	c.deliver = deliver
	defer func() { c.deliver = nil }()
	for len(b) > 0 {
		h, err := parseRecordHeader(b)
		if err != nil {
			// Drop the rest of a malformed datagram.
			return nil
		}
		record := b[:recordHeaderSize+h.length]
		b = b[len(record):]
		if err := c.handleRecord(h, record, now); err != nil {
			return c.fail(err)
		}
		if c.err != nil {
			return c.err
		}
		if c.state == stateEstablished && len(c.queuedAppDat) > 0 {
			// Application data that came ahead of the end of the handshake.
			queued := c.queuedAppDat
			c.queuedAppDat = nil
			for _, data := range queued {
				deliver(data)
			}
		}
	}
	return nil
}

func (c *Conn) handleRecord(h recordHeader, record []byte, now time.Time) error {
	payload := record[recordHeaderSize:]
	if h.version != version12 && h.version != version10 {
		return nil
	}
	switch {
	case h.epoch == 0:
		switch h.typ {
		case contentHandshake:
			return c.handleHandshakeRecord(0, payload, now)
		case contentChangeCipherSpec:
			return c.handleChangeCipherSpec(payload, now)
		case contentAlert:
			if c.readCipher != nil {
				// An unprotected alert after keys are in place may be forged.
				return nil
			}
			return c.handleAlert(payload)
		}
		return nil
	case h.epoch == 1:
		if c.readCipher == nil {
			if len(c.queued) < maxQueued && c.state != stateStart {
				c.queued = append(c.queued, bytes.Clone(record))
			}
			return nil
		}
		if !c.replay.fresh(h.seq) {
			return nil
		}
		plain, err := c.readCipher.open(h, payload)
		if err != nil {
			// RFC 6347 section 4.1.2.7: drop records that fail
			// authentication.
			return nil
		}
		c.replay.accept(h.seq)
		switch h.typ {
		case contentHandshake:
			return c.handleHandshakeRecord(1, plain, now)
		case contentAlert:
			return c.handleAlert(plain)
		case contentApplicationData:
			if c.state != stateEstablished {
				if len(c.queuedAppDat) < maxQueued {
					c.queuedAppDat = append(c.queuedAppDat, bytes.Clone(plain))
				}
				return nil
			}
			c.deliver(plain)
		}
	}
	return nil
}

func (c *Conn) handleAlert(b []byte) error {
	if len(b) != 2 {
		return nil
	}
	if b[1] == alertCloseNotify {
		c.state = stateFailed
		c.err = ErrClosed
		return nil
	}
	if b[0] == 2 {
		c.state = stateFailed
		c.err = &AlertError{Description: b[1], Remote: true}
	}
	return nil
}

func (c *Conn) handleChangeCipherSpec(b []byte, now time.Time) error {
	if len(b) != 1 || b[0] != 1 {
		return &AlertError{Description: alertDecodeError}
	}
	if c.readCipher != nil || c.state == stateEstablished {
		return nil
	}
	// It may come ahead of the messages that bring the keys: the switch
	// then waits for them.
	c.ccsReceived = true
	return c.installReadCipher(now)
}

// installReadCipher switches reading to epoch 1 once both the keys and the
// peer's ChangeCipherSpec are there, then processes the records of epoch 1
// that came early.
func (c *Conn) installReadCipher(now time.Time) error {
	if !c.ccsReceived || c.pendingRead == nil || c.readCipher != nil {
		return nil
	}
	c.readCipher = c.pendingRead
	c.readEpoch = 1
	queued := c.queued
	c.queued = nil
	for _, record := range queued {
		h, err := parseRecordHeader(record)
		if err != nil {
			continue
		}
		if err := c.handleRecord(h, record, now); err != nil {
			return err
		}
	}
	return nil
}

func (c *Conn) handleHandshakeRecord(epoch uint16, b []byte, now time.Time) error {
	for len(b) > 0 {
		h, ok := parseHandshakeHeader(b)
		if !ok {
			return &AlertError{Description: alertDecodeError}
		}
		fragment := b[handshakeHeaderSize : handshakeHeaderSize+h.fragmentLength]
		b = b[handshakeHeaderSize+h.fragmentLength:]
		if err := c.handleFragment(epoch, h, fragment, now); err != nil {
			return err
		}
	}
	return nil
}

func (c *Conn) handleFragment(epoch uint16, h handshakeHeader, fragment []byte, now time.Time) error {
	if c.state == stateWaitClientHello && h.typ == typeClientHello && h.messageSeq < c.msgSeqRecv {
		// A server that sent a HelloVerifyRequest receives the first
		// ClientHello again if the request was lost.
		c.msgSeqRecv = h.messageSeq
	}
	if h.messageSeq < c.msgSeqRecv {
		c.peerRetransmitted(now)
		return nil
	}
	if h.messageSeq >= c.msgSeqRecv+reorderWindow {
		return nil
	}
	var p *partial
	for _, q := range c.partials {
		if q.seq == h.messageSeq {
			p = q
			break
		}
	}
	if p == nil {
		p = &partial{seq: h.messageSeq, typ: h.typ, epoch: epoch, length: h.length, body: make([]byte, h.length)}
		c.partials = append(c.partials, p)
	} else if p.typ != h.typ || p.length != h.length {
		return &AlertError{Description: alertDecodeError}
	}
	copy(p.body[h.fragmentOffset:], fragment)
	p.add(h.fragmentOffset, h.fragmentOffset+h.fragmentLength)

	for {
		var next *partial
		for i, q := range c.partials {
			if q.seq == c.msgSeqRecv && q.complete() {
				next = q
				c.partials = append(c.partials[:i], c.partials[i+1:]...)
				break
			}
		}
		if next == nil {
			return nil
		}
		c.msgSeqRecv++
		if err := c.handleMessage(next, now); err != nil {
			return err
		}
		if c.err != nil {
			return nil
		}
	}
}

// peerRetransmitted sends the last flight again when the peer repeats a
// flight already processed: it did not receive ours.
func (c *Conn) peerRetransmitted(now time.Time) {
	if len(c.flight) == 0 || now.Sub(c.lastResend) < 100*time.Millisecond {
		return
	}
	c.sendFlight(now, false)
}

// addTranscript appends a handshake message to the transcript as an
// unfragmented message.
func (c *Conn) addTranscript(typ byte, seq uint16, body []byte) {
	c.transcript = appendHandshakeHeader(c.transcript, typ, len(body), seq, 0, len(body))
	c.transcript = append(c.transcript, body...)
}

func (c *Conn) handleMessage(m *partial, now time.Time) error {
	if m.typ == typeFinished {
		if m.epoch != 1 {
			return &AlertError{Description: alertUnexpectedMessage}
		}
	} else if m.epoch != 0 && c.state != stateEstablished {
		return &AlertError{Description: alertUnexpectedMessage}
	}
	if c.client {
		return c.clientMessage(m, now)
	}
	return c.serverMessage(m, now)
}

func (c *Conn) clientMessage(m *partial, now time.Time) error {
	switch {
	case c.state == stateWaitServerHello && m.typ == typeHelloVerifyRequest:
		cookie, ok := parseHelloVerifyRequest(m.body)
		if !ok || len(cookie) > 255 {
			return &AlertError{Description: alertDecodeError}
		}
		// RFC 6347 section 4.2.1: the first ClientHello and the request
		// are left out of the transcript.
		c.transcript = c.transcript[:0]
		c.hello.cookie = bytes.Clone(cookie)
		c.newFlight()
		c.queueHandshake(0, typeClientHello, c.hello.marshal())
		c.sendFlight(now, true)
		return nil
	case c.state == stateWaitServerHello && m.typ == typeServerHello:
		hello, ok := parseServerHello(m.body)
		if !ok {
			return &AlertError{Description: alertDecodeError}
		}
		if hello.version != version12 {
			return &AlertError{Description: alertProtocolVersion}
		}
		if c.suite = suiteByID(hello.cipherSuite); c.suite == nil || hello.compression != 0 {
			return &AlertError{Description: alertIllegalParameter}
		}
		c.addTranscript(m.typ, m.seq, m.body)
		c.serverRandom = hello.random
		c.extendedMaster = hello.ext.extendedMasterSecret
		c.state = stateWaitServerHelloDone
		return nil
	case c.state == stateWaitServerHelloDone && m.typ == typeCertificate:
		chain, ok := parseCertificate(m.body)
		if !ok {
			return &AlertError{Description: alertDecodeError}
		}
		c.addTranscript(m.typ, m.seq, m.body)
		return c.acceptPeerChain(chain)
	case c.state == stateWaitServerHelloDone && m.typ == typeServerKeyExchange:
		ske, ok := parseServerKeyExchange(m.body)
		if !ok {
			return &AlertError{Description: alertDecodeError}
		}
		if c.peerKey == nil {
			return &AlertError{Description: alertUnexpectedMessage}
		}
		signed := make([]byte, 0, 64+len(ske.params))
		signed = append(append(append(signed, c.clientRandom[:]...), c.serverRandom[:]...), ske.params...)
		if verify(c.peerKey, ske.signature, signed, ske.sig) != nil {
			return &AlertError{Description: alertDecryptError}
		}
		curve, ok := curveFor(ske.group)
		if !ok {
			return &AlertError{Description: alertIllegalParameter}
		}
		peer, err := curve.NewPublicKey(ske.public)
		if err != nil {
			return &AlertError{Description: alertIllegalParameter}
		}
		if c.ecdhKey, err = curve.GenerateKey(rand.Reader); err != nil {
			return &AlertError{Description: alertInternalError}
		}
		if c.premaster, err = c.ecdhKey.ECDH(peer); err != nil {
			return &AlertError{Description: alertIllegalParameter}
		}
		c.addTranscript(m.typ, m.seq, m.body)
		return nil
	case c.state == stateWaitServerHelloDone && m.typ == typeCertificateRequest:
		req, ok := parseCertificateRequest(m.body)
		if !ok {
			return &AlertError{Description: alertDecodeError}
		}
		c.certRequested = true
		c.requestedSignatures = req.signatures
		c.addTranscript(m.typ, m.seq, m.body)
		return nil
	case c.state == stateWaitServerHelloDone && m.typ == typeServerHelloDone:
		if len(m.body) != 0 || c.premaster == nil {
			return &AlertError{Description: alertUnexpectedMessage}
		}
		c.addTranscript(m.typ, m.seq, m.body)
		return c.sendClientFinished(now)
	case c.state == stateWaitServerFinished && m.typ == typeFinished:
		want := verifyData(c.suite, c.master[:], false, c.transcript)
		if !hmac.Equal(want, m.body) {
			return &AlertError{Description: alertDecryptError}
		}
		c.addTranscript(m.typ, m.seq, m.body)
		c.established()
		return nil
	case c.state == stateEstablished:
		// Renegotiation is not supported; a repeated server flight was
		// handled as a retransmission already.
		return nil
	}
	return &AlertError{Description: alertUnexpectedMessage}
}

// sendClientFinished sends the second client flight: the certificate if
// asked for, the key exchange, the certificate verification, the switch to
// epoch 1 and the Finished message.
func (c *Conn) sendClientFinished(now time.Time) error {
	c.newFlight()
	if c.certRequested {
		c.queueHandshake(0, typeCertificate, marshalCertificate(c.cfg.Certificate.Chain))
	}
	c.queueHandshake(0, typeClientKeyExchange, appendVec8(nil, c.ecdhKey.PublicKey().Bytes()))
	if err := c.deriveKeys(now); err != nil {
		return err
	}
	if c.certRequested {
		scheme, ok := chooseScheme(c.requestedSignatures)
		if !ok {
			return &AlertError{Description: alertHandshakeFailure}
		}
		sig, err := sign(c.cfg.Certificate.Key, scheme, c.transcript)
		if err != nil {
			return &AlertError{Description: alertInternalError}
		}
		c.queueHandshake(0, typeCertificateVerify, digitallySigned{scheme: scheme, sig: sig}.marshal())
	}
	c.flight = append(c.flight, flightMessage{ccs: true})
	c.queueHandshake(1, typeFinished, verifyData(c.suite, c.master[:], true, c.transcript))
	c.state = stateWaitServerFinished
	c.sendFlight(now, true)
	return nil
}

func (c *Conn) serverMessage(m *partial, now time.Time) error {
	switch {
	case c.state == stateWaitClientHello && m.typ == typeClientHello:
		return c.handleClientHello(m, now)
	case c.state == stateWaitClientFinished && m.typ == typeCertificate:
		chain, ok := parseCertificate(m.body)
		if !ok {
			return &AlertError{Description: alertDecodeError}
		}
		c.addTranscript(m.typ, m.seq, m.body)
		return c.acceptPeerChain(chain)
	case c.state == stateWaitClientFinished && m.typ == typeClientKeyExchange:
		if c.peerKey == nil {
			// WebRTC requires the client certificate.
			return &AlertError{Description: alertHandshakeFailure}
		}
		r := newReader(m.body)
		public := r.vec8()
		if !r.done() {
			return &AlertError{Description: alertDecodeError}
		}
		peer, err := c.ecdhKey.Curve().NewPublicKey(public)
		if err != nil {
			return &AlertError{Description: alertIllegalParameter}
		}
		if c.premaster, err = c.ecdhKey.ECDH(peer); err != nil {
			return &AlertError{Description: alertIllegalParameter}
		}
		c.addTranscript(m.typ, m.seq, m.body)
		c.verifyLen = len(c.transcript)
		return c.deriveKeys(now)
	case c.state == stateWaitClientFinished && m.typ == typeCertificateVerify:
		if c.premaster == nil || c.peerKey == nil {
			return &AlertError{Description: alertUnexpectedMessage}
		}
		d, ok := parseDigitallySigned(m.body)
		if !ok {
			return &AlertError{Description: alertDecodeError}
		}
		if verify(c.peerKey, d.scheme, c.transcript[:c.verifyLen], d.sig) != nil {
			return &AlertError{Description: alertDecryptError}
		}
		c.addTranscript(m.typ, m.seq, m.body)
		c.verifyLen = -1
		return nil
	case c.state == stateWaitClientFinished && m.typ == typeFinished:
		if c.verifyLen != -1 {
			// The certificate was never proven.
			return &AlertError{Description: alertHandshakeFailure}
		}
		want := verifyData(c.suite, c.master[:], true, c.transcript)
		if !hmac.Equal(want, m.body) {
			return &AlertError{Description: alertDecryptError}
		}
		c.addTranscript(m.typ, m.seq, m.body)
		c.newFlight()
		c.flight = append(c.flight, flightMessage{ccs: true})
		c.queueHandshake(1, typeFinished, verifyData(c.suite, c.master[:], false, c.transcript))
		c.sendFlight(now, false)
		c.established()
		return nil
	case c.state == stateEstablished:
		if m.typ == typeClientHello {
			c.sendAlert(1, alertNoRenegotiation)
		}
		return nil
	}
	return &AlertError{Description: alertUnexpectedMessage}
}

func (c *Conn) handleClientHello(m *partial, now time.Time) error {
	hello, ok := parseClientHello(m.body)
	if !ok {
		return &AlertError{Description: alertDecodeError}
	}
	// DTLS versions count down: anything above 1.2 is older.
	if hello.version > version12 {
		return &AlertError{Description: alertProtocolVersion}
	}
	if c.cfg.HelloVerify {
		cookie := c.cookie(hello)
		if !hmac.Equal(cookie, hello.cookie) {
			// Stateless: nothing of this ClientHello is kept, and the
			// request is not part of any flight.
			var rec []byte
			body := marshalHelloVerifyRequest(cookie)
			rec = appendHandshakeHeader(rec, typeHelloVerifyRequest, len(body), m.seq, 0, len(body))
			rec = append(rec, body...)
			c.writeRecord(0, contentHandshake, rec)
			c.flushDatagram()
			return nil
		}
	}
	for _, id := range hello.cipherSuites {
		if c.suite = suiteByID(id); c.suite != nil {
			break
		}
	}
	if c.suite == nil || !bytes.Contains(hello.compression, []byte{0}) {
		return &AlertError{Description: alertHandshakeFailure}
	}
	group, ok := chooseGroup(hello.ext.groups)
	if !ok {
		return &AlertError{Description: alertHandshakeFailure}
	}
	signatures := hello.ext.signatures
	if len(signatures) == 0 {
		signatures = []uint16{sigECDSAP256SHA256}
	}
	scheme, ok := chooseScheme(signatures)
	if !ok {
		return &AlertError{Description: alertHandshakeFailure}
	}
	curve, _ := curveFor(group)
	var err error
	if c.ecdhKey, err = curve.GenerateKey(rand.Reader); err != nil {
		return &AlertError{Description: alertInternalError}
	}
	c.clientRandom = hello.random
	_, _ = rand.Read(c.serverRandom[:])
	c.extendedMaster = hello.ext.extendedMasterSecret
	c.addTranscript(m.typ, m.seq, m.body)

	renegotiation := hello.ext.renegotiationInfo
	for _, id := range hello.cipherSuites {
		renegotiation = renegotiation || id == scsvRenegotiation
	}
	reply := &serverHello{
		version:     version12,
		random:      c.serverRandom,
		cipherSuite: c.suite.id,
		ext: extensions{
			extendedMasterSecret: c.extendedMaster,
			renegotiationInfo:    renegotiation,
		},
	}
	if len(hello.ext.pointFormats) > 0 {
		reply.ext.pointFormats = []byte{0}
	}
	params := marshalECDHEParams(group, c.ecdhKey.PublicKey().Bytes())
	signed := make([]byte, 0, 64+len(params))
	signed = append(append(append(signed, c.clientRandom[:]...), c.serverRandom[:]...), params...)
	sig, err := sign(c.cfg.Certificate.Key, scheme, signed)
	if err != nil {
		return &AlertError{Description: alertInternalError}
	}

	// The server numbers its messages from the ClientHello it answers.
	c.msgSeqSend = m.seq
	c.newFlight()
	c.queueHandshake(0, typeServerHello, reply.marshal())
	c.queueHandshake(0, typeCertificate, marshalCertificate(c.cfg.Certificate.Chain))
	c.queueHandshake(0, typeServerKeyExchange, append(params, digitallySigned{scheme: scheme, sig: sig}.marshal()...))
	c.queueHandshake(0, typeCertificateRequest, marshalCertificateRequest())
	c.queueHandshake(0, typeServerHelloDone, nil)
	c.state = stateWaitClientFinished
	c.sendFlight(now, true)
	return nil
}

// cookie computes the HelloVerifyRequest cookie of a ClientHello.
func (c *Conn) cookie(hello *clientHello) []byte {
	mac := hmac.New(sha256.New, c.cookieKey[:])
	mac.Write(hello.random[:])
	mac.Write(hello.sessionID)
	for _, s := range hello.cipherSuites {
		mac.Write(binary.BigEndian.AppendUint16(nil, s))
	}
	mac.Write(hello.compression)
	return mac.Sum(nil)[:20]
}

func (c *Conn) acceptPeerChain(chain [][]byte) error {
	if c.cfg.VerifyPeer == nil || c.cfg.VerifyPeer(chain) != nil {
		return &AlertError{Description: alertBadCertificate}
	}
	key, err := peerPublicKey(chain)
	if err != nil {
		return &AlertError{Description: alertBadCertificate}
	}
	c.peerChain = chain
	c.peerKey = key
	return nil
}

// deriveKeys computes the master secret and the record keys of epoch 1
// after the ClientKeyExchange.
func (c *Conn) deriveKeys(now time.Time) error {
	h := c.suite.hash()
	h.Write(c.transcript)
	c.master = masterSecret(c.suite, c.premaster, h.Sum(nil), c.clientRandom[:], c.serverRandom[:], c.extendedMaster)
	k := expandKeys(c.suite, c.master[:], c.clientRandom[:], c.serverRandom[:])
	writeKey, writeIV, readKey, readIV := k.serverKey, k.serverIV, k.clientKey, k.clientIV
	if c.client {
		writeKey, writeIV, readKey, readIV = k.clientKey, k.clientIV, k.serverKey, k.serverIV
	}
	var err error
	if c.writeCipher, err = newCipherState(writeKey, writeIV); err != nil {
		return &AlertError{Description: alertInternalError}
	}
	if c.pendingRead, err = newCipherState(readKey, readIV); err != nil {
		return &AlertError{Description: alertInternalError}
	}
	return c.installReadCipher(now)
}

func (c *Conn) established() {
	c.state = stateEstablished
	c.retransmit = time.Time{}
	c.deadline = time.Time{}
	c.premaster = nil
	c.partials = nil
	if c.client {
		// The server sent the last flight: nothing of ours is resent.
		c.flight = nil
	}
}

func (c *Conn) newFlight() {
	c.flight = c.flight[:0]
}

func (c *Conn) queueHandshake(epoch uint16, typ byte, body []byte) {
	seq := c.msgSeqSend
	c.msgSeqSend++
	c.flight = append(c.flight, flightMessage{epoch: epoch, typ: typ, seq: seq, body: body})
	c.addTranscript(typ, seq, body)
}

// sendFlight sends the last flight, fragmenting messages to the MTU, with
// fresh record sequence numbers. A new flight restarts the retransmission
// timer when it expects an answer.
func (c *Conn) sendFlight(now time.Time, first bool) {
	for _, m := range c.flight {
		if m.ccs {
			c.writeRecord(0, contentChangeCipherSpec, []byte{1})
			continue
		}
		room := c.mtu - recordHeaderSize - handshakeHeaderSize
		if m.epoch == 1 {
			room -= sealOverhead
		}
		for off := 0; ; {
			n := min(room, len(m.body)-off)
			payload := appendHandshakeHeader(make([]byte, 0, handshakeHeaderSize+n), m.typ, len(m.body), m.seq, off, n)
			payload = append(payload, m.body[off:off+n]...)
			c.writeRecord(m.epoch, contentHandshake, payload)
			if off += n; off >= len(m.body) {
				break
			}
		}
	}
	c.flushDatagram()
	c.lastResend = now
	if first {
		c.interval = initialRetransmit
		if c.state == stateEstablished {
			c.retransmit = time.Time{}
		} else {
			c.retransmit = now.Add(c.interval)
		}
	}
}

// writeRecord appends a record to the datagram being built, sending the
// datagram first if the record does not fit.
func (c *Conn) writeRecord(epoch uint16, typ byte, payload []byte) {
	size := recordHeaderSize + len(payload)
	if epoch == 1 {
		size += sealOverhead
	}
	if len(c.out) > 0 && len(c.out)+size > c.mtu {
		c.flushDatagram()
	}
	seq := c.writeSeq[epoch]
	c.writeSeq[epoch]++
	if epoch == 0 {
		c.out = appendRecordHeader(c.out, typ, 0, seq, len(payload))
		c.out = append(c.out, payload...)
		return
	}
	c.out = c.writeCipher.seal(c.out, typ, 1, seq, payload)
}

func (c *Conn) flushDatagram() {
	if len(c.out) == 0 {
		return
	}
	c.send(c.out)
	c.out = c.out[:0]
}

// Write sends data as an application data record in a datagram of its own.
func (c *Conn) Write(data []byte) error {
	if c.err != nil {
		return c.err
	}
	if c.state != stateEstablished {
		return errors.New("dtls: handshake not complete")
	}
	if c.writeSeq[1] >= maxSequence {
		return c.fail(errors.New("dtls: sequence numbers exhausted"))
	}
	c.writeRecord(1, contentApplicationData, data)
	c.flushDatagram()
	return nil
}

// Overhead returns how many bytes a record adds to the data it carries.
func Overhead() int { return recordHeaderSize + sealOverhead }

// Close sends a close_notify alert and closes the Conn.
func (c *Conn) Close() {
	if c.err != nil {
		return
	}
	c.sendAlert(1, alertCloseNotify)
	c.state = stateFailed
	c.err = ErrClosed
}

func (c *Conn) sendAlert(level, description byte) {
	epoch := uint16(0)
	if c.writeCipher != nil && c.state == stateEstablished {
		epoch = 1
	}
	c.writeRecord(epoch, contentAlert, []byte{level, description})
	c.flushDatagram()
}

// fail records err as the error of the Conn, sending a fatal alert for
// errors it raised itself.
func (c *Conn) fail(err error) error {
	if c.err != nil {
		return c.err
	}
	var alert *AlertError
	if errors.As(err, &alert) && !alert.Remote {
		c.sendAlert(2, alert.Description)
	}
	c.state = stateFailed
	c.err = err
	return err
}

func chooseGroup(offered []uint16) (uint16, bool) {
	if len(offered) == 0 {
		return groupP256, true
	}
	for _, g := range supportedGroups {
		for _, o := range offered {
			if o == g {
				return g, true
			}
		}
	}
	return 0, false
}

func curveFor(group uint16) (ecdh.Curve, bool) {
	switch group {
	case groupX25519:
		return ecdh.X25519(), true
	case groupP256:
		return ecdh.P256(), true
	case groupP384:
		return ecdh.P384(), true
	}
	return nil, false
}
