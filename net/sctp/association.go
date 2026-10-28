// Package sctp implements the SCTP of WebRTC data channels (RFC 9260 over
// DTLS, RFC 8261): one path, partial reliability (RFC 3758), stream reset
// (RFC 6525) and the channel establishment protocol of RFC 8832.
//
// An Association is a state machine without goroutines or I/O, after the
// design of dcSCTP: the owner feeds it received packets and timer expiries
// with the current time and gets packets to send through a callback. It is
// not safe for concurrent use.
package sctp

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"slices"
	"time"
)

// Config configures an Association.
type Config struct {
	// LocalPort and RemotePort are the SCTP ports, 5000 in WebRTC.
	LocalPort, RemotePort uint16
	// MaxPacketSize bounds the packets sent. Zero means 1163: a 1200 byte
	// datagram less the DTLS record overhead.
	MaxPacketSize int
	// ReceiveWindow is the receive window advertised. Zero means 1 MiB.
	ReceiveWindow uint32
	// SendBuffer bounds the bytes queued or in flight. Zero means 1 MiB.
	SendBuffer int
	// MaxRetransmissions is how many retransmission timeouts in a row
	// fail the association. Zero means 10.
	MaxRetransmissions int
	// OnMessage is called with each message received. data is valid until
	// it returns.
	OnMessage func(stream uint16, ppid uint32, data []byte)
	// OnStreamReset is called when the peer reset streams it sends on, as
	// closing a data channel does.
	OnStreamReset func(streams []uint16)
	// OnResetDone is called when a reset of streams this side sends on,
	// asked for with ResetStreams, completed.
	OnResetDone func(streams []uint16)
}

// SendOptions are the options of a message.
type SendOptions struct {
	// Unordered delivers the message as soon as it arrives.
	Unordered bool
	// Partial makes the message partially reliable (RFC 3758): its chunks
	// are sent at most MaxRetransmits times again, and not again once
	// Lifetime has passed since Send if Lifetime is set.
	Partial        bool
	MaxRetransmits int
	Lifetime       time.Duration
}

type assocState int

const (
	stateClosed assocState = iota
	stateCookieWait
	stateCookieEchoed
	stateEstablished
	// The shutdown states of RFC 9260 section 9.2.
	stateShutdownPending
	stateShutdownReceived
	stateShutdownSent
	stateShutdownAckSent
	stateDone
)

const (
	rtoInitial      = 500 * time.Millisecond
	rtoMin          = 400 * time.Millisecond
	rtoMax          = 60 * time.Second
	sackDelay       = 200 * time.Millisecond
	maxInitRetrans  = 8
	maxGapBlocks    = 32
	maxDuplicates   = 16
	initialCwndMTUs = 10
	// maxOutOfOrder bounds the TSNs held above the cumulative TSN, and
	// tsnWindow how far above it a TSN may be, so that a peer cannot grow
	// the receive state without bound.
	maxOutOfOrder = 8192
	tsnWindow     = 1 << 20
)

var (
	// ErrBufferFull is returned by Send when the message does not fit the
	// send buffer.
	ErrBufferFull = errors.New("sctp: send buffer full")
	// ErrClosed is the error of an association closed by either side.
	ErrClosed = errors.New("sctp: association closed")
	// ErrAborted is the error of an association the peer aborted.
	ErrAborted = errors.New("sctp: association aborted by peer")
	// ErrTimeout is the error of an association whose peer stopped
	// answering.
	ErrTimeout = errors.New("sctp: peer stopped answering")
	// ErrStreamResetting is returned by Send for a stream being reset.
	ErrStreamResetting = errors.New("sctp: stream is being reset")
	errState           = errors.New("sctp: association not established")
	errEmpty           = errors.New("sctp: empty message")
	// errSkipRest stops the processing of a packet at an unknown chunk.
	errSkipRest = errors.New("sctp: skip the rest of the packet")
)

// outMessage is a message being sent.
type outMessage struct {
	stream uint16
	ssn    uint16
	ppid   uint32
	opts   SendOptions
	data   []byte
	// next is the offset of the first byte not yet cut into a chunk.
	next      int
	expires   time.Time
	abandoned bool
}

// expired reports whether a partially reliable message outlived its
// lifetime.
func (m *outMessage) expired(now time.Time) bool {
	return !m.expires.IsZero() && !now.Before(m.expires)
}

// outChunk is a DATA chunk sent and not yet acknowledged cumulatively.
type outChunk struct {
	tsn        uint32
	msg        *outMessage
	flags      byte
	data       []byte
	transmits  int
	acked      bool
	abandoned  bool
	retransmit bool
	fastDone   bool
	misses     int
	inFlight   bool
}

func (c *outChunk) size() int { return dataHeaderSize + len(c.data) }

// inFragment is a fragment of a message being reassembled.
type inFragment struct {
	tsn    uint32
	stream uint16
	ssn    uint16
	ppid   uint32
	flags  byte
	data   []byte
}

// inStream is the receiving side of a stream.
type inStream struct {
	nextSSN uint16
	held    []inFragment
}

// Association is one SCTP association.
type Association struct {
	cfg   Config
	send  func([]byte)
	state assocState
	err   error

	localTag, peerTag uint32
	cookie            []byte
	// peerInit holds the INIT of the peer an INIT ACK answered.
	peerInit   initChunk
	haveInit   bool
	initAck    []byte
	initPacket []byte
	initialTSN uint32
	t1         time.Time
	t1Retrans  int
	t1Interval time.Duration
	forwardTSN bool
	reconfig   bool
	maxPayload int
	packet     packetBuilder
	chunks     []chunk
	gapScratch []uint16
	control    [][]byte
	cookieEcho []byte
	t2         time.Time
	t2Retrans  int

	// Sending.
	nextTSN      uint32
	cumAck       uint32
	advancedAck  uint32
	ssn          map[uint16]uint16
	pending      []*outMessage
	outstanding  []*outChunk
	buffered     int
	flight       int
	cwnd         int
	ssthresh     int
	partialAcked int
	peerWindow   int
	fastRecovery bool
	recoverTSN   uint32
	t3           time.Time
	rto          time.Duration
	srtt, rttvar time.Duration
	rttTSN       uint32
	rttSent      time.Time
	rttActive    bool
	errorCount   int
	forwardDue   bool

	// Receiving.
	peerCum     uint32
	received    []uint32
	dups        []uint32
	streams     map[uint16]*inStream
	unordered   []inFragment
	held        int
	sackNow     bool
	sackAt      time.Time
	dataPackets int

	reset resetState
}

// New returns an association waiting for the peer to open it. Connect
// opens it from this side.
func New(cfg Config, send func([]byte)) *Association {
	if cfg.LocalPort == 0 {
		cfg.LocalPort = 5000
	}
	if cfg.RemotePort == 0 {
		cfg.RemotePort = 5000
	}
	if cfg.MaxPacketSize == 0 {
		cfg.MaxPacketSize = 1163
	}
	if cfg.ReceiveWindow == 0 {
		cfg.ReceiveWindow = 1 << 20
	}
	if cfg.SendBuffer == 0 {
		cfg.SendBuffer = 1 << 20
	}
	if cfg.MaxRetransmissions == 0 {
		cfg.MaxRetransmissions = 10
	}
	a := &Association{
		cfg:     cfg,
		send:    send,
		ssn:     make(map[uint16]uint16),
		streams: make(map[uint16]*inStream),
		rto:     rtoInitial,
		// A chunk is padded to four bytes: the largest one must still fit
		// a packet of its own.
		maxPayload: (cfg.MaxPacketSize-commonHeaderSize)&^3 - dataHeaderSize,
	}
	a.localTag = randomNonZero()
	a.initialTSN = randomUint32()
	a.nextTSN = a.initialTSN
	a.cumAck = a.nextTSN - 1
	a.advancedAck = a.cumAck
	a.reset.nextOut = a.initialTSN
	a.cwnd = initialCwndMTUs * cfg.MaxPacketSize
	a.ssthresh = 1 << 30
	return a
}

func randomUint32() uint32 {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return binary.BigEndian.Uint32(b[:])
}

func randomNonZero() uint32 {
	for {
		if v := randomUint32(); v != 0 {
			return v
		}
	}
}

// Established reports whether the association is open for new messages.
func (a *Association) Established() bool { return a.state == stateEstablished }

// Err returns the error the association closed with, if any.
func (a *Association) Err() error { return a.err }

// Buffered returns the bytes of the messages queued or in flight.
func (a *Association) Buffered() int { return a.buffered }

// Available returns how many more bytes of messages Send accepts.
func (a *Association) Available() int { return max(0, a.cfg.SendBuffer-a.buffered) }

// RTT returns the smoothed round trip time, zero before a measurement.
func (a *Association) RTT() time.Duration { return a.srtt }

// Connect opens the association from this side by sending an INIT.
func (a *Association) Connect(now time.Time) {
	if a.state != stateClosed {
		return
	}
	init := initChunk{
		tag:        a.localTag,
		window:     a.cfg.ReceiveWindow,
		outStreams: 65535,
		inStreams:  65535,
		tsn:        a.initialTSN,
	}
	a.packet.start(header{srcPort: a.cfg.LocalPort, dstPort: a.cfg.RemotePort})
	a.packet.addChunk(chunkInit, 0, init.marshal())
	a.initPacket = bytes.Clone(a.packet.finish())
	a.state = stateCookieWait
	a.t1Interval = a.rto
	a.t1Retrans = 0
	a.t1 = now.Add(a.t1Interval)
	a.send(a.initPacket)
}

// Deadline returns when HandleTimeout must next be called, or the zero time.
func (a *Association) Deadline() time.Time {
	if a.err != nil {
		return time.Time{}
	}
	var d time.Time
	for _, t := range []time.Time{a.t1, a.t2, a.t3, a.sackAt, a.reset.timer} {
		if !t.IsZero() && (d.IsZero() || t.Before(d)) {
			d = t
		}
	}
	return d
}

// HandleTimeout runs the timers that expired.
func (a *Association) HandleTimeout(now time.Time) error {
	if a.err != nil {
		return a.err
	}
	if !a.t1.IsZero() && !now.Before(a.t1) {
		if a.t1Retrans++; a.t1Retrans > maxInitRetrans {
			return a.fail(ErrTimeout)
		}
		a.t1Interval = min(2*a.t1Interval, rtoMax)
		a.t1 = now.Add(a.t1Interval)
		switch a.state {
		case stateCookieWait:
			a.send(a.initPacket)
		case stateCookieEchoed:
			a.sendCookieEcho()
		}
	}
	if !a.t3.IsZero() && !now.Before(a.t3) {
		if err := a.retransmitTimeout(now); err != nil {
			return err
		}
	}
	if !a.t2.IsZero() && !now.Before(a.t2) {
		if a.t2Retrans++; a.t2Retrans > a.cfg.MaxRetransmissions {
			return a.fail(ErrTimeout)
		}
		a.t2 = now.Add(a.rto)
		switch a.state {
		case stateShutdownSent:
			a.queueShutdown()
		case stateShutdownAckSent:
			a.queueControl(chunkShutdownAck, 0, nil)
		}
		a.flush(now)
	}
	if !a.reset.timer.IsZero() && !now.Before(a.reset.timer) {
		a.reset.timer = now.Add(a.rto)
		a.queueResetRequest()
		a.flush(now)
	}
	if !a.sackAt.IsZero() && !now.Before(a.sackAt) {
		a.sackNow = true
		a.sackAt = time.Time{}
		a.flush(now)
	}
	return a.err
}

// HandlePacket processes a received packet.
func (a *Association) HandlePacket(b []byte, now time.Time) error {
	if a.err != nil && !errors.Is(a.err, ErrClosed) {
		return a.err
	}
	h, chunks, err := parsePacket(b, a.chunks)
	a.chunks = chunks[:0]
	if err != nil || h.dstPort != a.cfg.LocalPort || h.srcPort != a.cfg.RemotePort {
		return a.err
	}
	if a.err != nil {
		a.handleOutOfTheBlue(h, chunks)
		return a.err
	}
	if len(chunks) > 0 && chunks[0].typ == chunkInit {
		// An INIT travels alone with a zero tag.
		if h.tag == 0 && len(chunks) == 1 {
			a.handleInit(chunks[0])
		}
		return nil
	}
	for i, c := range chunks {
		if !a.tagValid(h.tag, c, i) {
			return nil
		}
		if err := a.handleChunk(c, now); err != nil {
			if errors.Is(err, errSkipRest) {
				break
			}
			return a.fail(err)
		}
		if a.err != nil {
			return a.err
		}
	}
	a.afterPacket(now)
	return a.err
}

// handleOutOfTheBlue answers a packet that arrives after the association
// closed: a SHUTDOWN ACK repeated because our SHUTDOWN COMPLETE was lost
// gets another one, with the T bit (RFC 9260 section 8.4).
func (a *Association) handleOutOfTheBlue(h header, chunks []chunk) {
	for _, c := range chunks {
		if c.typ == chunkShutdownAck && h.tag == a.localTag {
			a.packet.start(header{srcPort: a.cfg.LocalPort, dstPort: a.cfg.RemotePort, tag: h.tag})
			a.packet.addChunk(chunkShutdownComplete, 1)
			a.send(a.packet.finish())
			return
		}
	}
}

// tagValid checks the verification tag of a packet for one of its chunks.
func (a *Association) tagValid(tag uint32, c chunk, index int) bool {
	switch c.typ {
	case chunkAbort, chunkShutdownComplete:
		if c.flags&1 != 0 {
			// The T bit: the sender reflected our own tag.
			return tag == a.peerTag
		}
	case chunkCookieEcho:
		// A COOKIE ECHO carries the tag of the INIT ACK it answers; the
		// cookie is checked on its own.
		return index == 0 && tag == a.localTag
	}
	return tag == a.localTag
}

func (a *Association) handleChunk(c chunk, now time.Time) error {
	switch c.typ {
	case chunkInitAck:
		a.handleInitAck(c, now)
	case chunkCookieEcho:
		a.handleCookieEcho(c)
	case chunkCookieAck:
		if a.state == stateCookieEchoed {
			a.establish()
		}
	case chunkData:
		return a.handleData(c)
	case chunkSack:
		return a.handleSack(c, now)
	case chunkForwardTSN:
		a.handleForwardTSN(c)
	case chunkHeartbeat:
		if a.state >= stateEstablished {
			a.queueControl(chunkHeartbeatAck, 0, c.value)
		}
	case chunkReconfig:
		a.handleReconfig(c)
	case chunkAbort:
		a.state = stateDone
		a.err = ErrAborted
	case chunkShutdown:
		return a.handleShutdown(c, now)
	case chunkShutdownAck:
		if a.state == stateShutdownSent || a.state == stateShutdownAckSent {
			a.queueControl(chunkShutdownComplete, 0, nil)
			a.flushControl()
			a.close(ErrClosed)
		}
	case chunkShutdownComplete:
		if a.state == stateShutdownAckSent {
			a.close(ErrClosed)
		}
	case chunkHeartbeatAck, chunkError:
	default:
		if c.typ&0x80 == 0 {
			// Unknown chunk types with the top bit clear stop the
			// processing of the packet.
			return errSkipRest
		}
	}
	return nil
}

func (a *Association) handleInit(c chunk) {
	peer, ok := parseInit(c.value)
	if !ok {
		return
	}
	switch a.state {
	case stateClosed, stateCookieWait, stateCookieEchoed:
		// RFC 9260 section 5.2.1: an INIT crossing ours is answered with
		// the tag and TSN of our own INIT, so that both handshakes agree.
	default:
		// The peer restarted: this package does not support restarts.
		return
	}
	if a.haveInit && peer.tag == a.peerInit.tag && a.initAck != nil {
		// The INIT ACK was lost: send it again.
		a.send(a.initAck)
		return
	}
	a.peerInit = peer
	a.haveInit = true
	a.cookie = make([]byte, 32)
	_, _ = rand.Read(a.cookie)
	ack := initChunk{
		tag:        a.localTag,
		window:     a.cfg.ReceiveWindow,
		outStreams: 65535,
		inStreams:  65535,
		tsn:        a.initialTSN,
		cookie:     a.cookie,
	}
	a.packet.start(header{srcPort: a.cfg.LocalPort, dstPort: a.cfg.RemotePort, tag: peer.tag})
	a.packet.addChunk(chunkInitAck, 0, ack.marshal())
	a.initAck = bytes.Clone(a.packet.finish())
	a.send(a.initAck)
}

func (a *Association) handleInitAck(c chunk, now time.Time) {
	if a.state != stateCookieWait {
		return
	}
	peer, ok := parseInit(c.value)
	if !ok || peer.cookie == nil {
		return
	}
	a.adopt(peer)
	a.cookieEcho = bytes.Clone(peer.cookie)
	a.state = stateCookieEchoed
	a.t1Retrans = 0
	a.t1Interval = a.rto
	a.t1 = now.Add(a.t1Interval)
	a.sendCookieEcho()
}

func (a *Association) sendCookieEcho() {
	a.packet.start(header{srcPort: a.cfg.LocalPort, dstPort: a.cfg.RemotePort, tag: a.peerTag})
	a.packet.addChunk(chunkCookieEcho, 0, a.cookieEcho)
	a.send(a.packet.finish())
}

func (a *Association) handleCookieEcho(c chunk) {
	if a.cookie == nil || !bytes.Equal(c.value, a.cookie) {
		return
	}
	switch a.state {
	case stateClosed, stateCookieWait, stateCookieEchoed:
		// RFC 9260 section 5.2.4: in a collision the cookie of our INIT
		// ACK settles the peer's tag and TSN.
		a.adopt(a.peerInit)
		a.establish()
	}
	a.queueControl(chunkCookieAck, 0, nil)
}

// adopt takes the parameters of the peer's INIT or INIT ACK.
func (a *Association) adopt(peer initChunk) {
	a.peerTag = peer.tag
	a.peerCum = peer.tsn - 1
	a.peerWindow = int(peer.window)
	a.forwardTSN = peer.forwardTSN
	a.reconfig = peer.reconfig
	a.reset.nextIn = peer.tsn
	a.received = a.received[:0]
}

func (a *Association) establish() {
	if a.state == stateEstablished {
		return
	}
	a.state = stateEstablished
	a.t1 = time.Time{}
	a.cookieEcho = nil
	a.initPacket = nil
}

// queueControl queues a control chunk for the next packet.
func (a *Association) queueControl(typ, flags byte, value []byte) {
	b := make([]byte, 0, chunkSize(len(value)))
	b = append(b, typ, flags)
	b = binary.BigEndian.AppendUint16(b, uint16(chunkHeaderSize+len(value)))
	b = append(b, value...)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	a.control = append(a.control, b)
}

// Send queues a message on a stream. The message must not be empty: data
// channels send empty messages as a single byte with an empty PPID.
func (a *Association) Send(stream uint16, ppid uint32, data []byte, opts SendOptions, now time.Time) error {
	if a.err != nil {
		return a.err
	}
	if a.state != stateEstablished {
		return errState
	}
	if len(data) == 0 {
		return errEmpty
	}
	if a.reset.resetting(stream) {
		return ErrStreamResetting
	}
	if a.buffered+len(data) > a.cfg.SendBuffer {
		return ErrBufferFull
	}
	m := &outMessage{stream: stream, ppid: ppid, opts: opts, data: bytes.Clone(data)}
	if opts.Partial && opts.Lifetime > 0 {
		m.expires = now.Add(opts.Lifetime)
	}
	if !opts.Unordered {
		m.ssn = a.ssn[stream]
		a.ssn[stream] = m.ssn + 1
	}
	a.pending = append(a.pending, m)
	a.buffered += len(data)
	a.flush(now)
	return nil
}

func (a *Association) handleData(c chunk) error {
	switch a.state {
	case stateEstablished, stateShutdownPending, stateShutdownSent:
	default:
		return nil
	}
	if len(c.value) < 13 {
		// A DATA chunk carries at least one byte.
		return errPacket
	}
	f := inFragment{
		tsn:    binary.BigEndian.Uint32(c.value),
		stream: binary.BigEndian.Uint16(c.value[4:]),
		ssn:    binary.BigEndian.Uint16(c.value[6:]),
		ppid:   binary.BigEndian.Uint32(c.value[8:]),
		flags:  c.flags,
		data:   c.value[12:],
	}
	a.dataPackets++
	if c.flags&flagImmediate != 0 {
		a.sackNow = true
	}
	if tsnLessEq(f.tsn, a.peerCum) {
		a.duplicate(f.tsn)
		return nil
	}
	i, found := slices.BinarySearchFunc(a.received, f.tsn, compareTSN)
	if found {
		a.duplicate(f.tsn)
		return nil
	}
	if f.tsn != a.peerCum+1 {
		if f.tsn-a.peerCum > tsnWindow || len(a.received) >= maxOutOfOrder || a.held+len(f.data) > int(a.cfg.ReceiveWindow) {
			// No room: the peer sends it again.
			return nil
		}
		a.received = slices.Insert(a.received, i, f.tsn)
		// Out of order: tell the peer about the gap at once.
		a.sackNow = true
	} else {
		// RFC 9260 section 6.7: while gaps remain, and when one closes, every
		// packet is acknowledged at once, or the sender's loss recovery
		// waits for the delayed SACK on each retransmission.
		if len(a.received) > 0 {
			a.sackNow = true
		}
		a.peerCum = f.tsn
		a.absorbReceived()
	}
	a.receive(f)
	a.performPendingReset()
	return nil
}

func compareTSN(x, t uint32) int {
	if x == t {
		return 0
	}
	if tsnLess(x, t) {
		return -1
	}
	return 1
}

// absorbReceived moves the cumulative TSN over the TSNs received above it.
func (a *Association) absorbReceived() {
	n := 0
	for n < len(a.received) && tsnLessEq(a.received[n], a.peerCum+1) {
		if a.received[n] == a.peerCum+1 {
			a.peerCum = a.received[n]
		}
		n++
	}
	a.received = slices.Delete(a.received, 0, n)
}

func (a *Association) duplicate(tsn uint32) {
	if len(a.dups) < maxDuplicates {
		a.dups = append(a.dups, tsn)
	}
	a.sackNow = true
}

// receive delivers a fragment or holds it until its message completes and,
// for an ordered stream, its turn comes.
func (a *Association) receive(f inFragment) {
	whole := f.flags&(flagBegin|flagEnd) == flagBegin|flagEnd
	if f.flags&flagUnordered != 0 {
		if whole {
			a.deliver(f.stream, f.ppid, f.data)
			return
		}
		f.data = bytes.Clone(f.data)
		a.held += len(f.data)
		a.unordered = insertFragment(a.unordered, f)
		a.assembleUnordered()
		return
	}
	s := a.streams[f.stream]
	if s == nil {
		s = &inStream{}
		a.streams[f.stream] = s
	}
	if whole && f.ssn == s.nextSSN {
		s.nextSSN++
		a.deliver(f.stream, f.ppid, f.data)
	} else {
		if ssnLess(f.ssn, s.nextSSN) {
			return
		}
		f.data = bytes.Clone(f.data)
		a.held += len(f.data)
		s.held = insertFragment(s.held, f)
	}
	a.assembleOrdered(f.stream, s)
}

func insertFragment(list []inFragment, f inFragment) []inFragment {
	i, _ := slices.BinarySearchFunc(list, f.tsn, func(x inFragment, t uint32) int { return compareTSN(x.tsn, t) })
	return slices.Insert(list, i, f)
}

// assembleOrdered delivers the held messages of a stream whose turn came.
func (a *Association) assembleOrdered(stream uint16, s *inStream) {
	for len(s.held) > 0 {
		start := -1
		for i, f := range s.held {
			if f.ssn == s.nextSSN && f.flags&flagBegin != 0 {
				start = i
				break
			}
		}
		if start < 0 {
			return
		}
		end, ok := messageEnd(s.held, start)
		if !ok {
			return
		}
		a.deliverFragments(stream, s.held[start:end+1])
		s.held = slices.Delete(s.held, start, end+1)
		s.nextSSN++
	}
}

// assembleUnordered delivers the unordered messages whose fragments all
// arrived.
func (a *Association) assembleUnordered() {
	for i := 0; i < len(a.unordered); i++ {
		if a.unordered[i].flags&flagBegin == 0 {
			continue
		}
		end, ok := messageEnd(a.unordered, i)
		if !ok {
			continue
		}
		a.deliverFragments(a.unordered[i].stream, a.unordered[i:end+1])
		a.unordered = slices.Delete(a.unordered, i, end+1)
		i--
	}
}

// messageEnd returns the index of the last fragment of the message that
// starts at list[start], if all its fragments are held. The fragments of a
// message have consecutive TSNs (RFC 9260 section 6.9).
func messageEnd(list []inFragment, start int) (int, bool) {
	for i := start; i < len(list); i++ {
		if i > start && (list[i].tsn != list[i-1].tsn+1 || list[i].flags&flagBegin != 0 || list[i].stream != list[start].stream) {
			return 0, false
		}
		if list[i].flags&flagEnd != 0 {
			return i, true
		}
	}
	return 0, false
}

func (a *Association) deliverFragments(stream uint16, frags []inFragment) {
	size := 0
	for _, f := range frags {
		size += len(f.data)
	}
	data := make([]byte, 0, size)
	for _, f := range frags {
		data = append(data, f.data...)
	}
	a.held -= size
	a.deliver(stream, frags[0].ppid, data)
}

func (a *Association) deliver(stream uint16, ppid uint32, data []byte) {
	if a.cfg.OnMessage != nil {
		a.cfg.OnMessage(stream, ppid, data)
	}
}

func (a *Association) handleForwardTSN(c chunk) {
	if len(c.value) < 4 || (len(c.value)-4)%4 != 0 {
		return
	}
	newCum := binary.BigEndian.Uint32(c.value)
	a.sackNow = true
	if !tsnLess(a.peerCum, newCum) || newCum-a.peerCum > tsnWindow {
		return
	}
	a.peerCum = newCum
	a.absorbReceived()
	// Drop the fragments the peer gave up on.
	a.unordered = a.dropFragments(a.unordered, newCum)
	for off := 4; off+4 <= len(c.value); off += 4 {
		stream := binary.BigEndian.Uint16(c.value[off:])
		ssn := binary.BigEndian.Uint16(c.value[off+2:])
		s := a.streams[stream]
		if s == nil {
			s = &inStream{}
			a.streams[stream] = s
		}
		s.held = a.dropFragments(s.held, newCum)
		if !ssnLess(ssn, s.nextSSN) {
			s.nextSSN = ssn + 1
		}
		a.assembleOrdered(stream, s)
	}
	a.performPendingReset()
}

func (a *Association) dropFragments(list []inFragment, cum uint32) []inFragment {
	return slices.DeleteFunc(list, func(f inFragment) bool {
		if tsnLessEq(f.tsn, cum) {
			a.held -= len(f.data)
			return true
		}
		return false
	})
}

func (a *Association) handleSack(c chunk, now time.Time) error {
	switch a.state {
	case stateEstablished, stateShutdownPending, stateShutdownReceived, stateShutdownSent, stateShutdownAckSent:
	default:
		return nil
	}
	s, ok := parseSack(c.value, a.gapScratch)
	if !ok {
		return errPacket
	}
	a.gapScratch = s.gaps[:0]
	if tsnLess(s.cumTSN, a.cumAck) {
		// An old SACK, reordered.
		return nil
	}
	if !tsnLess(s.cumTSN, a.nextTSN) {
		// It acknowledges data never sent.
		return errPacket
	}
	fullyUsed := a.flight >= a.cwnd-a.cfg.MaxPacketSize
	advanced := tsnLess(a.cumAck, s.cumTSN)
	newly, highest, haveHighest := a.ackCumulative(s.cumTSN, now)
	// Gap acknowledged chunks are marked; a chunk acknowledged by an
	// earlier SACK and not by this one was reneged on and is unacknowledged
	// again (RFC 9260 section 6.2.1).
	var highestGap uint32
	haveGap, reneged := false, false
	for _, ch := range a.outstanding {
		covered := gapCovers(s, ch.tsn)
		if covered && (!haveGap || tsnLess(highestGap, ch.tsn)) {
			highestGap, haveGap = ch.tsn, true
		}
		switch {
		case covered && !ch.acked:
			ch.acked = true
			ch.retransmit = false
			if ch.inFlight {
				a.flight -= ch.size()
				ch.inFlight = false
			}
			if !ch.abandoned {
				newly += len(ch.data)
			}
			if ch.transmits == 1 && a.rttActive && ch.tsn == a.rttTSN {
				a.measure(now.Sub(a.rttSent))
			}
			if !haveHighest || tsnLess(highest, ch.tsn) {
				highest, haveHighest = ch.tsn, true
			}
		case !covered && ch.acked:
			ch.acked = false
			ch.misses = 0
			reneged = true
		}
	}
	if newly > 0 || advanced {
		a.errorCount = 0
	}
	// Miss indications follow the highest TSN newly acknowledged, so that
	// a SACK repeating the same gaps adds nothing; in fast recovery an
	// advancing cumulative ack counts every reported gap (RFC 9260 section
	// 7.2.4).
	switch {
	case a.fastRecovery && advanced && haveGap:
		a.countMisses(highestGap)
	case haveHighest:
		a.countMisses(highest)
	}
	if a.fastRecovery && !tsnLess(a.cumAck, a.recoverTSN) {
		a.fastRecovery = false
	}
	// Congestion window growth (RFC 9260 sections 7.2.1 and 7.2.2).
	if advanced && !a.fastRecovery && fullyUsed {
		if a.cwnd <= a.ssthresh {
			a.cwnd += min(newly, a.cfg.MaxPacketSize)
		} else {
			a.partialAcked += newly
			if a.partialAcked >= a.cwnd {
				a.partialAcked -= a.cwnd
				a.cwnd += a.cfg.MaxPacketSize
			}
		}
	}
	a.peerWindow = max(0, int(s.window)-a.flight)
	a.advanceForwardTSN()
	a.updateT3(now, advanced || reneged)
	a.maybeShutdown(now)
	return nil
}

// gapCovers reports whether a gap ack block of a SACK covers tsn.
func gapCovers(s sackChunk, tsn uint32) bool {
	for i := 0; i+1 < len(s.gaps); i += 2 {
		start, end := s.cumTSN+uint32(s.gaps[i]), s.cumTSN+uint32(s.gaps[i+1])
		if tsnLessEq(start, tsn) && tsnLessEq(tsn, end) {
			return true
		}
	}
	return false
}

// ackCumulative releases the chunks up to cum, returning the bytes newly
// acknowledged and the highest TSN newly acknowledged.
func (a *Association) ackCumulative(cum uint32, now time.Time) (newly int, highest uint32, haveHighest bool) {
	drop := 0
	for _, ch := range a.outstanding {
		if !tsnLessEq(ch.tsn, cum) {
			break
		}
		if !ch.acked && !ch.abandoned {
			newly += len(ch.data)
			highest, haveHighest = ch.tsn, true
			if ch.transmits == 1 && a.rttActive && ch.tsn == a.rttTSN {
				a.measure(now.Sub(a.rttSent))
			}
		}
		a.release(ch)
		drop++
	}
	a.outstanding = slices.Delete(a.outstanding, 0, drop)
	if tsnLess(a.cumAck, cum) {
		a.cumAck = cum
	}
	if tsnLess(a.advancedAck, a.cumAck) {
		a.advancedAck = a.cumAck
	}
	return newly, highest, haveHighest
}

// updateT3 keeps the retransmission timer running while data is in flight
// or unacknowledged, or a FORWARD TSN waits for its acknowledgement, and
// restarts it when the acknowledgement advanced.
func (a *Association) updateT3(now time.Time, restart bool) {
	switch {
	case a.flight == 0 && !a.anyUnacked() && !tsnLess(a.cumAck, a.advancedAck):
		a.t3 = time.Time{}
	case restart || a.t3.IsZero():
		a.t3 = now.Add(a.rto)
	}
}

// countMisses counts a miss for the chunks below highest that are still
// unacknowledged and marks those missed three times for fast retransmit.
func (a *Association) countMisses(highest uint32) {
	for _, ch := range a.outstanding {
		if !tsnLess(ch.tsn, highest) {
			break
		}
		if ch.acked || ch.abandoned || ch.fastDone || ch.retransmit {
			continue
		}
		if ch.misses++; ch.misses < 3 {
			continue
		}
		ch.fastDone = true
		if !a.fastRecovery {
			a.fastRecovery = true
			a.recoverTSN = a.nextTSN - 1
			a.ssthresh = max(a.cwnd/2, 4*a.cfg.MaxPacketSize)
			a.cwnd = a.ssthresh
			a.partialAcked = 0
		}
		a.markRetransmit(ch, time.Time{})
	}
}

// markRetransmit marks a chunk lost: it leaves the flight and is sent
// again, unless its message ran out of retransmissions or of time.
func (a *Association) markRetransmit(ch *outChunk, now time.Time) {
	if a.rttActive && ch.tsn == a.rttTSN {
		// Karn's rule: a chunk sent twice gives no sample.
		a.rttActive = false
	}
	if ch.inFlight {
		a.flight -= ch.size()
		ch.inFlight = false
	}
	m := ch.msg
	if m.opts.Partial && (ch.transmits > m.opts.MaxRetransmits || !now.IsZero() && m.expired(now)) {
		a.abandon(m)
		return
	}
	ch.retransmit = true
}

// abandon gives up a message of partial reliability: its chunks are not
// sent again and FORWARD TSN tells the peer to skip them.
func (a *Association) abandon(m *outMessage) {
	if m.abandoned {
		return
	}
	m.abandoned = true
	for _, ch := range a.outstanding {
		if ch.msg != m || ch.abandoned {
			continue
		}
		ch.abandoned = true
		ch.retransmit = false
		if ch.inFlight {
			a.flight -= ch.size()
			ch.inFlight = false
		}
	}
	// The part never cut into chunks is dropped from the queue.
	if m.next < len(m.data) {
		a.buffered -= len(m.data) - m.next
		m.next = len(m.data)
	}
	a.forwardDue = true
}

// advanceForwardTSN moves the advanced peer ack point over the abandoned
// chunks that follow the cumulative ack and queues a FORWARD TSN.
func (a *Association) advanceForwardTSN() {
	if !a.forwardTSN {
		return
	}
	moved := false
	for _, ch := range a.outstanding {
		if tsnLessEq(ch.tsn, a.advancedAck) {
			continue
		}
		if ch.tsn != a.advancedAck+1 || !ch.abandoned {
			break
		}
		a.advancedAck = ch.tsn
		moved = true
	}
	if moved || (a.forwardDue && tsnLess(a.cumAck, a.advancedAck)) {
		a.queueForwardTSN()
	}
	a.forwardDue = false
}

func (a *Association) queueForwardTSN() {
	v := binary.BigEndian.AppendUint32(nil, a.advancedAck)
	last := map[uint16]uint16{}
	for _, ch := range a.outstanding {
		if tsnLess(a.advancedAck, ch.tsn) {
			break
		}
		if ch.abandoned && !ch.msg.opts.Unordered {
			if ssn, ok := last[ch.msg.stream]; !ok || ssnLess(ssn, ch.msg.ssn) {
				last[ch.msg.stream] = ch.msg.ssn
			}
		}
	}
	streams := make([]uint16, 0, len(last))
	for stream := range last {
		streams = append(streams, stream)
	}
	slices.Sort(streams)
	for _, stream := range streams {
		v = binary.BigEndian.AppendUint16(v, stream)
		v = binary.BigEndian.AppendUint16(v, last[stream])
	}
	a.queueControl(chunkForwardTSN, 0, v)
}

func (a *Association) anyUnacked() bool {
	for _, ch := range a.outstanding {
		if !ch.acked && !ch.abandoned {
			return true
		}
	}
	return false
}

// release frees the send buffer space of an acknowledged chunk.
func (a *Association) release(ch *outChunk) {
	if ch.inFlight {
		a.flight -= ch.size()
		ch.inFlight = false
	}
	a.buffered -= len(ch.data)
	ch.data = nil
}

// measure updates the round trip estimate of RFC 6298.
func (a *Association) measure(r time.Duration) {
	a.rttActive = false
	if a.srtt == 0 {
		a.srtt, a.rttvar = r, r/2
	} else {
		diff := a.srtt - r
		if diff < 0 {
			diff = -diff
		}
		a.rttvar = (3*a.rttvar + diff) / 4
		a.srtt = (7*a.srtt + r) / 8
	}
	a.rto = min(max(a.srtt+4*a.rttvar, rtoMin), rtoMax)
}

// retransmitTimeout handles the expiry of the retransmission timer: the
// window collapses to one packet and everything in flight is sent again.
func (a *Association) retransmitTimeout(now time.Time) error {
	if a.errorCount++; a.errorCount > a.cfg.MaxRetransmissions {
		return a.fail(ErrTimeout)
	}
	a.ssthresh = max(a.cwnd/2, 4*a.cfg.MaxPacketSize)
	a.cwnd = a.cfg.MaxPacketSize
	a.partialAcked = 0
	a.fastRecovery = false
	a.rto = min(2*a.rto, rtoMax)
	a.rttActive = false
	for _, ch := range a.outstanding {
		if !ch.acked && !ch.abandoned {
			a.markRetransmit(ch, now)
		}
	}
	// A FORWARD TSN the peer has not acted on goes again.
	a.forwardDue = true
	a.advanceForwardTSN()
	a.t3 = now.Add(a.rto)
	a.flush(now)
	a.updateT3(now, false)
	a.maybeShutdown(now)
	return nil
}

// afterPacket sends what processing a packet called for: a SACK, control
// chunks, data the acknowledgements made room for.
func (a *Association) afterPacket(now time.Time) {
	if a.dataPackets > 0 {
		if a.dataPackets >= 2 {
			a.sackNow = true
		}
		if !a.sackNow && a.sackAt.IsZero() {
			a.sackAt = now.Add(sackDelay)
		}
		if a.state == stateShutdownSent {
			// RFC 9260 section 9.2: DATA in SHUTDOWN-SENT is answered with
			// a SHUTDOWN alongside the SACK.
			a.sackNow = true
			a.queueShutdown()
			a.t2 = now.Add(a.rto)
		}
	}
	a.flush(now)
}

// sending reports whether the association still sends data.
func (a *Association) sending() bool {
	return a.state == stateEstablished || a.state == stateShutdownPending || a.state == stateShutdownReceived
}

// flush sends packets of control chunks, a SACK, retransmissions and new
// data, as much as the windows allow.
func (a *Association) flush(now time.Time) {
	if a.state < stateEstablished && len(a.control) == 0 {
		return
	}
	for {
		a.packet.start(header{srcPort: a.cfg.LocalPort, dstPort: a.cfg.RemotePort, tag: a.peerTag})
		limit := a.cfg.MaxPacketSize
		for len(a.control) > 0 && a.packet.size()+len(a.control[0]) <= limit {
			a.packet.buf = append(a.packet.buf, a.control[0]...)
			a.control = a.control[1:]
		}
		if a.sackNow || !a.sackAt.IsZero() && (len(a.pending) > 0 || a.retransmitting()) {
			// A delayed SACK rides along with data.
			a.addSack()
		}
		sentData := a.sending() && a.addData(now, limit)
		if a.packet.empty() {
			break
		}
		a.send(a.packet.finish())
		if !sentData && len(a.control) == 0 && !a.moreToSend() {
			break
		}
	}
	a.sendResetRequestWhenReady(now)
}

func (a *Association) retransmitting() bool {
	for _, ch := range a.outstanding {
		if ch.retransmit {
			return true
		}
	}
	return false
}

func (a *Association) moreToSend() bool {
	if !a.sending() {
		return false
	}
	if a.retransmitting() {
		return a.flight < a.cwnd
	}
	return len(a.pending) > 0 && a.flight < a.cwnd && (a.peerWindow > 0 || a.flight == 0)
}

func (a *Association) addSack() {
	gaps := a.gapScratch[:0]
	for i := 0; i < len(a.received) && len(gaps) < 2*maxGapBlocks; {
		j := i
		for j+1 < len(a.received) && a.received[j+1] == a.received[j]+1 {
			j++
		}
		gaps = append(gaps, uint16(a.received[i]-a.peerCum), uint16(a.received[j]-a.peerCum))
		i = j + 1
	}
	window := uint32(max(0, int(a.cfg.ReceiveWindow)-a.held))
	v := appendSack(make([]byte, 0, 12+2*len(gaps)+4*len(a.dups)), a.peerCum, window, gaps, a.dups)
	a.gapScratch = gaps[:0]
	a.packet.addChunk(chunkSack, 0, v)
	a.dups = a.dups[:0]
	a.sackNow = false
	a.sackAt = time.Time{}
	a.dataPackets = 0
}

// addData adds retransmissions and new DATA chunks to the packet being
// built, within the congestion and receive windows, and reports whether it
// added any.
func (a *Association) addData(now time.Time, limit int) bool {
	added := false
	for _, ch := range a.outstanding {
		if !ch.retransmit {
			continue
		}
		if a.flight >= a.cwnd && added {
			return true
		}
		if a.packet.size()+chunkSize(12+len(ch.data)) > limit {
			return added
		}
		a.writeData(ch, now)
		ch.retransmit = false
		added = true
	}
	for len(a.pending) > 0 {
		m := a.pending[0]
		if !m.abandoned && m.expired(now) && m.next > 0 {
			// Some chunks went out: the peer must be told to skip them.
			a.abandon(m)
		}
		if m.abandoned || m.next >= len(m.data) || m.expired(now) {
			if m.next < len(m.data) {
				// Expired before any of it was sent: nothing to skip.
				a.buffered -= len(m.data) - m.next
				m.next = len(m.data)
				m.abandoned = true
			}
			a.pending = a.pending[1:]
			continue
		}
		if a.flight >= a.cwnd || (a.peerWindow <= 0 && a.flight > 0) {
			return added
		}
		room := (limit-a.packet.size())&^3 - dataHeaderSize
		if room < 64 && !(room > 0 && len(m.data)-m.next <= room) {
			return added
		}
		n := min(len(m.data)-m.next, a.maxPayload, room)
		var flags byte
		if m.next == 0 {
			flags |= flagBegin
		}
		if m.next+n == len(m.data) {
			flags |= flagEnd
		}
		if m.opts.Unordered {
			flags |= flagUnordered
		}
		ch := &outChunk{tsn: a.nextTSN, msg: m, flags: flags, data: m.data[m.next : m.next+n]}
		a.nextTSN++
		m.next += n
		if m.next == len(m.data) {
			a.pending = a.pending[1:]
		}
		a.outstanding = append(a.outstanding, ch)
		if !a.rttActive {
			a.rttActive, a.rttTSN, a.rttSent = true, ch.tsn, now
		}
		a.writeData(ch, now)
		a.peerWindow -= ch.size()
		added = true
	}
	return added
}

func (a *Association) writeData(ch *outChunk, now time.Time) {
	var head [12]byte
	binary.BigEndian.PutUint32(head[:], ch.tsn)
	binary.BigEndian.PutUint16(head[4:], ch.msg.stream)
	binary.BigEndian.PutUint16(head[6:], ch.msg.ssn)
	binary.BigEndian.PutUint32(head[8:], ch.msg.ppid)
	a.packet.addChunk(chunkData, ch.flags, head[:], ch.data)
	ch.transmits++
	if !ch.inFlight {
		ch.inFlight = true
		a.flight += ch.size()
	}
	if a.t3.IsZero() {
		a.t3 = now.Add(a.rto)
	}
}

// Shutdown closes the association gracefully: the messages queued and in
// flight are delivered first, then the SHUTDOWN exchange of RFC 9260
// section 9.2 runs.
func (a *Association) Shutdown(now time.Time) {
	switch a.state {
	case stateEstablished:
		a.state = stateShutdownPending
		a.maybeShutdown(now)
	case stateShutdownPending, stateShutdownReceived, stateShutdownSent, stateShutdownAckSent, stateDone:
	default:
		a.Abort()
	}
}

// drained reports whether every message sent was acknowledged.
func (a *Association) drained() bool {
	return len(a.pending) == 0 && len(a.outstanding) == 0
}

// maybeShutdown moves a shutdown along once the data is acknowledged.
func (a *Association) maybeShutdown(now time.Time) {
	if !a.drained() {
		return
	}
	switch a.state {
	case stateShutdownPending:
		a.state = stateShutdownSent
		a.t2Retrans = 0
		a.t2 = now.Add(a.rto)
		a.queueShutdown()
		a.flush(now)
	case stateShutdownReceived:
		a.state = stateShutdownAckSent
		a.t2Retrans = 0
		a.t2 = now.Add(a.rto)
		a.queueControl(chunkShutdownAck, 0, nil)
		a.flush(now)
	}
}

func (a *Association) queueShutdown() {
	a.queueControl(chunkShutdown, 0, binary.BigEndian.AppendUint32(nil, a.peerCum))
}

// handleShutdown answers a SHUTDOWN: its cumulative TSN acknowledges like
// a SACK, and once this side's data is acknowledged a SHUTDOWN ACK follows.
func (a *Association) handleShutdown(c chunk, now time.Time) error {
	if len(c.value) != 4 {
		return errPacket
	}
	switch a.state {
	case stateEstablished, stateShutdownPending:
		a.state = stateShutdownReceived
	case stateShutdownReceived, stateShutdownAckSent:
	case stateShutdownSent:
		// Both sides shut down at once.
		a.state = stateShutdownAckSent
		a.t2Retrans = 0
		a.t2 = now.Add(a.rto)
		a.queueControl(chunkShutdownAck, 0, nil)
		return nil
	default:
		return nil
	}
	cum := binary.BigEndian.Uint32(c.value)
	if !tsnLess(cum, a.cumAck) && tsnLess(cum, a.nextTSN) {
		_, _, _ = a.ackCumulative(cum, now)
		a.updateT3(now, true)
	}
	if a.state == stateShutdownAckSent {
		a.queueControl(chunkShutdownAck, 0, nil)
	}
	a.maybeShutdown(now)
	return nil
}

// flushControl sends the queued control chunks alone.
func (a *Association) flushControl() {
	for len(a.control) > 0 {
		a.packet.start(header{srcPort: a.cfg.LocalPort, dstPort: a.cfg.RemotePort, tag: a.peerTag})
		for len(a.control) > 0 && a.packet.size()+len(a.control[0]) <= a.cfg.MaxPacketSize {
			a.packet.buf = append(a.packet.buf, a.control[0]...)
			a.control = a.control[1:]
		}
		a.send(a.packet.finish())
	}
}

// Abort closes the association at once with an ABORT chunk.
func (a *Association) Abort() {
	if a.state == stateDone {
		return
	}
	if a.peerTag != 0 {
		a.packet.start(header{srcPort: a.cfg.LocalPort, dstPort: a.cfg.RemotePort, tag: a.peerTag})
		a.packet.addChunk(chunkAbort, 0)
		a.send(a.packet.finish())
	}
	a.close(ErrClosed)
}

// close ends the association with err and stops its timers.
func (a *Association) close(err error) {
	a.state = stateDone
	if a.err == nil {
		a.err = err
	}
	a.t1, a.t2, a.t3, a.sackAt, a.reset.timer = time.Time{}, time.Time{}, time.Time{}, time.Time{}, time.Time{}
}

func (a *Association) fail(err error) error {
	if a.err != nil {
		return a.err
	}
	a.Abort()
	a.err = err
	return err
}
