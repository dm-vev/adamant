package nethernet

import (
	"bytes"
	"crypto/ecdsa"
	"errors"
	"io"
	"net"
	"net/netip"
	"os"
	"sync"
	"time"

	"github.com/dm-vev/adamant/net/dtls"
	"github.com/dm-vev/adamant/net/ice"
	"github.com/dm-vev/adamant/net/sctp"
)

// Labels of the data channels a NetherNet client opens.
const (
	labelReliable   = "ReliableDataChannel"
	labelUnreliable = "UnreliableDataChannel"
)

const (
	// maxMessageSize is the max-message-size offered: 256 KiB, as BDS and
	// the game offer it.
	maxMessageSize = 262144
	// maxSegments bounds the segments of a message: the game counts them
	// in a byte and sends at most 255.
	maxSegments = 255
	// maxReassembled bounds a message put back together from segments.
	maxReassembled = 8 << 20
	// queueBytes and queueLength bound the messages waiting for
	// ReadMessage: a peer that sends more than the reader takes is dropped.
	queueBytes  = 16 << 20
	queueLength = 4096
	// openTimeout drops a connection whose channels did not open in time.
	openTimeout = 20 * time.Second
	// lingerTimeout bounds the delivery of what was sent before Close.
	lingerTimeout = 5 * time.Second
	// defaultSendBuffer bounds the bytes queued on a connection.
	defaultSendBuffer = 16 << 20
	// maxBatchedDatagrams and maxBatchedBytes bound the datagrams sent in
	// one call, as the kernel bounds a segmented send.
	maxBatchedDatagrams = 64
	maxBatchedBytes     = 65000
)

var (
	// ErrBufferFull is returned by Send when the peer did not take enough
	// of what was sent before.
	ErrBufferFull = sctp.ErrBufferFull
	// ErrTooLarge is returned by Send for a message of more than 255
	// segments.
	ErrTooLarge = errors.New("nethernet: message too large")

	errPeerClosed  = errors.New("nethernet: peer closed the connection")
	errNotOpen     = errors.New("nethernet: data channel not open")
	errSegment     = errors.New("nethernet: malformed message segment")
	errQueueFull   = errors.New("nethernet: reader does not keep up with the peer")
	errOpenTimeout = errors.New("nethernet: data channels did not open in time")
	errSlowPeer    = errors.New("nethernet: peer does not take what is sent")
)

// channel is a data channel of the connection.
type channel struct {
	stream uint16
	open   bool
}

// connConfig configures a Conn.
type connConfig struct {
	// local and remote are the descriptions of both sides.
	local, remote *description
	// controlling makes the ICE agent check and nominate, as the dialing
	// side does.
	controlling bool
	// dtlsClient makes this side the DTLS client.
	dtlsClient bool
	// opener makes this side open the data channels, as the dialing side
	// does.
	opener      bool
	certificate dtls.Certificate
	sendBuffer  int
	// clock replaces time.Now and the timers of the connection in tests.
	clock func() time.Time
	out   func(b []byte, to netip.AddrPort)
	// outBatch, if set, sends datagrams of size bytes back to back in b, the
	// last maybe shorter, in one call.
	outBatch  func(b []byte, size int, to netip.AddrPort)
	localAddr net.Addr
}

// Conn is a NetherNet connection: the data channels a client opens, over
// SCTP, DTLS and ICE. Messages go on the reliable channel, split into
// segments of the size the peer accepts. Conn implements net.Conn, one
// message per Read and Write, and the packet transport methods gophertunnel
// looks for. Its methods are safe for concurrent use.
type Conn struct {
	mu       sync.Mutex
	clock    func() time.Time
	out      func(b []byte, to netip.AddrPort)
	outBatch func(b []byte, size int, to netip.AddrPort)
	// batch gathers the DTLS datagrams of the event being handled, to go
	// out in one call: batchSize bytes each but maybe the last.
	batch      []byte
	batchTo    netip.AddrPort
	batchSize  int
	batchCount int
	batchShort bool

	agent      *ice.Agent
	dtls       *dtls.Conn
	assoc      *sctp.Association
	dtlsClient bool
	started    bool
	connecting bool
	associated bool
	selected   netip.AddrPort
	remote     netip.AddrPort
	localAddr  net.Addr
	peerFP     []Fingerprint
	publicKey  *ecdsa.PublicKey
	maxSegment int
	// at is the time of the event being handled.
	at time.Time

	opener               bool
	reliable, unreliable channel
	ready                bool
	opened               chan struct{}
	segments             []byte
	remaining            int

	onReady    func(c *Conn)
	onClose    func(c *Conn)
	receiver   func(msg []byte)
	delivering bool
	pending    [][]byte
	// queue holds the messages waiting for ReadMessage from head on, and
	// queued their bytes; readable is signalled when one is queued.
	queue    [][]byte
	head     int
	queued   int
	readable chan struct{}

	closed       chan struct{}
	err          error
	linger       time.Time
	finished     bool
	expires      time.Time
	timer        *time.Timer
	armed        time.Time
	readDeadline time.Time

	// Listener state, guarded by the listener's lock.
	ufrag       string
	addrs       []netip.AddrPort
	pendingSlot bool
}

func newConn(cfg connConfig, now time.Time) *Conn {
	c := &Conn{
		clock:      cfg.clock,
		out:        cfg.out,
		outBatch:   cfg.outBatch,
		dtlsClient: cfg.dtlsClient,
		localAddr:  cfg.localAddr,
		peerFP:     cfg.remote.fingerprints,
		maxSegment: min(cfg.remote.maxMessageSize, maxMessageSize) - 1,
		opener:     cfg.opener,
		opened:     make(chan struct{}),
		readable:   make(chan struct{}, 1),
		closed:     make(chan struct{}),
		expires:    now.Add(openTimeout),
	}
	c.agent = ice.New(ice.Config{
		Controlling:      cfg.controlling,
		Local:            ice.Credentials{Ufrag: cfg.local.ufrag, Pwd: cfg.local.pwd},
		Remote:           ice.Credentials{Ufrag: cfg.remote.ufrag, Pwd: cfg.remote.pwd},
		RemoteCandidates: cfg.remote.candidates,
	}, cfg.out)
	dcfg := dtls.Config{Certificate: cfg.certificate, VerifyPeer: c.verifyPeer}
	if cfg.dtlsClient {
		c.dtls = dtls.Client(dcfg, c.sendDTLS)
	} else {
		c.dtls = dtls.Server(dcfg, c.sendDTLS)
	}
	sendBuffer := cfg.sendBuffer
	if sendBuffer == 0 {
		sendBuffer = defaultSendBuffer
	}
	c.assoc = sctp.New(sctp.Config{
		RemotePort:    uint16(cfg.remote.sctpPort),
		SendBuffer:    sendBuffer,
		OnMessage:     c.handleMessage,
		OnStreamReset: c.handleStreamReset,
	}, c.sendSCTP)
	c.agent.Start(now)
	return c
}

func (c *Conn) now() time.Time {
	if c.clock != nil {
		return c.clock()
	}
	return time.Now()
}

func (c *Conn) sendDTLS(b []byte) {
	if !c.remote.IsValid() {
		return
	}
	if c.outBatch == nil {
		c.out(b, c.remote)
		return
	}
	if c.batchCount > 0 && (c.batchTo != c.remote || c.batchShort || len(b) > c.batchSize ||
		len(c.batch)+len(b) > maxBatchedBytes || c.batchCount == maxBatchedDatagrams) {
		c.flushBatch()
	}
	if c.batchCount == 0 {
		c.batchTo, c.batchSize = c.remote, len(b)
		if c.batch == nil {
			c.batch = (*batchBuffers.Get().(*[]byte))[:0]
		}
	}
	c.batch = append(c.batch, b...)
	c.batchCount++
	c.batchShort = len(b) < c.batchSize
}

// batchBuffers holds the buffers of batches between events.
var batchBuffers = sync.Pool{New: func() any {
	b := make([]byte, 0, maxBatchedBytes)
	return &b
}}

// flushBatch sends the datagrams gathered.
func (c *Conn) flushBatch() {
	switch c.batchCount {
	case 0:
		return
	case 1:
		c.out(c.batch, c.batchTo)
	default:
		c.outBatch(c.batch, c.batchSize, c.batchTo)
	}
	b := c.batch[:0]
	batchBuffers.Put(&b)
	c.batch, c.batchCount, c.batchShort = nil, 0, false
}

func (c *Conn) sendSCTP(pkt []byte) {
	// A DTLS failure surfaces through HandleDatagram or HandleTimeout.
	_ = c.dtls.Write(pkt)
}

// verifyPeer checks the certificate of the peer against the fingerprints
// of its description.
func (c *Conn) verifyPeer(chain [][]byte) error {
	if len(chain) == 0 {
		return errors.New("nethernet: peer sent no certificate")
	}
	for _, fp := range c.peerFP {
		if dtls.MatchFingerprint(chain[0], fp.Algorithm, fp.Digest) {
			return nil
		}
	}
	return errors.New("nethernet: peer certificate does not match its fingerprint")
}

// handleDatagram processes a datagram received from addr: a STUN message
// or DTLS records. It reports whether it was a STUN message of the peer
// that proved addr.
func (c *Conn) handleDatagram(b []byte, from netip.AddrPort, now time.Time) (checked bool) {
	c.mu.Lock()
	defer c.release()
	if c.finished || len(b) == 0 {
		return false
	}
	c.at = now
	switch {
	case b[0] < 4:
		// RFC 7983: STUN.
		if !c.agent.HandleSTUN(b, from, now) {
			return false
		}
		checked = c.agent.Validated(from)
	case b[0] >= 20 && b[0] <= 63:
		// DTLS, accepted only from an address that proved the ICE password.
		if !c.agent.Validated(from) {
			return false
		}
		c.remote = from
		if err := c.dtls.HandleDatagram(b, now, c.receiveSCTP); err != nil {
			c.fail(dtlsError(err))
			return false
		}
		c.agent.Alive(now)
	default:
		return false
	}
	c.progress(now)
	return checked
}

// dtlsError names the close_notify of the peer as a close.
func dtlsError(err error) error {
	var alert *dtls.AlertError
	if errors.As(err, &alert) && alert.Remote && alert.Description == 0 {
		return errPeerClosed
	}
	return err
}

func (c *Conn) receiveSCTP(pkt []byte) {
	if c.finished {
		return
	}
	if err := c.assoc.HandlePacket(pkt, c.at); err != nil {
		c.associationDone(err)
	}
}

// associationDone closes the connection once its association ended.
func (c *Conn) associationDone(err error) {
	if errors.Is(err, sctp.ErrClosed) || errors.Is(err, sctp.ErrAborted) {
		err = errPeerClosed
	}
	c.shut(err, false)
}

// progress moves the layers along after an event: DTLS starts once ICE
// selected a path, SCTP once DTLS is up, and the channels open once SCTP is.
func (c *Conn) progress(now time.Time) {
	if c.finished {
		return
	}
	if addr, ok := c.agent.Selected(); ok && addr != c.selected {
		// Follow the path the controlling side nominates.
		c.selected, c.remote = addr, addr
		if !c.started {
			c.started = true
			c.dtls.Start(now)
		}
	}
	if c.dtls.Established() && !c.connecting {
		// Both sides open the association, as BDS and libwebrtc do: the
		// INITs cross and SCTP settles it.
		c.connecting = true
		c.assoc.Connect(now)
	}
	if c.assoc.Established() {
		c.associated = true
		if c.opener && !c.reliable.open {
			c.openChannels(now)
		}
	} else if c.associated && c.err == nil {
		// The peer shuts the association down.
		c.shut(errPeerClosed, true)
	}
	if !c.ready && c.reliable.open && c.err == nil {
		c.ready = true
		c.expires = time.Time{}
		close(c.opened)
		if c.onReady != nil {
			c.onReady(c)
		}
	}
	if !c.finished && !c.linger.IsZero() && c.assoc.Err() != nil {
		c.finish()
	}
	c.schedule()
}

// openChannels opens the two channels of NetherNet. The DTLS client takes
// even streams and the server odd ones (RFC 8832 section 6).
func (c *Conn) openChannels(now time.Time) {
	base := uint16(1)
	if c.dtlsClient {
		base = 0
	}
	c.reliable = channel{stream: base, open: true}
	c.unreliable = channel{stream: base + 2, open: true}
	reliable := sctp.ChannelOpen{Type: sctp.ChannelReliable, Label: labelReliable}
	unreliable := sctp.ChannelOpen{Type: sctp.ChannelPartialRexmitUnordered, Label: labelUnreliable}
	_ = c.assoc.Send(c.reliable.stream, sctp.PPIDControl, reliable.Marshal(), sctp.SendOptions{}, now)
	_ = c.assoc.Send(c.unreliable.stream, sctp.PPIDControl, unreliable.Marshal(), sctp.SendOptions{}, now)
}

// handleMessage handles a message of the association: a message of the
// channel protocol, or a segment of a NetherNet message.
func (c *Conn) handleMessage(stream uint16, ppid uint32, data []byte) {
	if c.err != nil {
		return
	}
	switch ppid {
	case sctp.PPIDControl:
		c.handleControl(stream, data)
	case sctp.PPIDBinary, sctp.PPIDString:
		switch {
		case c.reliable.open && stream == c.reliable.stream:
			c.handleSegment(data)
		case c.unreliable.open && stream == c.unreliable.stream:
			// Messages of the unreliable channel are never split.
			if len(data) > 1 && data[0] == 0 {
				c.deliver(bytes.Clone(data[1:]))
			}
		}
	}
}

func (c *Conn) handleControl(stream uint16, data []byte) {
	if sctp.IsChannelAck(data) {
		return
	}
	open, err := sctp.ParseChannelOpen(data)
	if err != nil || c.opener {
		return
	}
	switch {
	case open.Label == labelReliable && !c.reliable.open:
		if open.Unordered() || open.SendOptions().Partial {
			c.fail(errors.New("nethernet: reliable channel opened unreliable"))
			return
		}
		c.reliable = channel{stream: stream, open: true}
	case open.Label == labelUnreliable && !c.unreliable.open:
		c.unreliable = channel{stream: stream, open: true}
	default:
		// Another channel, or one opened again: not part of NetherNet.
		return
	}
	_ = c.assoc.Send(stream, sctp.PPIDControl, sctp.ChannelAck(), sctp.SendOptions{}, c.at)
}

// handleSegment puts a NetherNet message back together: the first byte of
// each segment counts the segments still to come.
func (c *Conn) handleSegment(data []byte) {
	if len(data) == 0 {
		c.fail(errSegment)
		return
	}
	left := int(data[0])
	if c.remaining > 0 && left != c.remaining-1 {
		c.fail(errSegment)
		return
	}
	if c.remaining == 0 && left == 0 {
		if len(data) > 1 {
			c.deliver(bytes.Clone(data[1:]))
		}
		return
	}
	if len(c.segments)+len(data)-1 > maxReassembled {
		c.fail(ErrTooLarge)
		return
	}
	c.segments = append(c.segments, data[1:]...)
	c.remaining = left
	if left == 0 {
		msg := c.segments
		c.segments = nil
		if len(msg) > 0 {
			c.deliver(msg)
		}
	}
}

// handleStreamReset closes the connection when the peer closes a channel.
func (c *Conn) handleStreamReset([]uint16) {
	c.shut(errPeerClosed, true)
}

// deliver hands a message the connection owns to the receiver, or queues
// it for ReadMessage.
func (c *Conn) deliver(msg []byte) {
	if c.err != nil {
		return
	}
	if c.receiver != nil {
		c.pending = append(c.pending, msg)
		return
	}
	if c.queued+len(msg) > queueBytes || len(c.queue)-c.head >= queueLength {
		c.fail(errQueueFull)
		return
	}
	c.queue = append(c.queue, msg)
	c.queued += len(msg)
	select {
	case c.readable <- struct{}{}:
	default:
	}
}

// dequeue takes the next message queued for ReadMessage, if any.
func (c *Conn) dequeue() ([]byte, bool) {
	if c.head == len(c.queue) {
		return nil, false
	}
	msg := c.queue[c.head]
	c.queue[c.head] = nil
	c.head++
	if c.head == len(c.queue) {
		// Empty: start over at the front of the array.
		c.queue, c.head = c.queue[:0], 0
	}
	c.queued -= len(msg)
	return msg, true
}

// release unlocks the connection and hands the messages received to the
// receiver set with SetReceiver, in order: one goroutine at a time does.
func (c *Conn) release() {
	c.flushBatch()
	if c.receiver == nil || c.delivering || len(c.pending) == 0 {
		c.mu.Unlock()
		return
	}
	c.delivering = true
	for len(c.pending) > 0 {
		batch, recv := c.pending, c.receiver
		c.pending = nil
		c.mu.Unlock()
		for _, msg := range batch {
			recv(msg)
		}
		c.mu.Lock()
	}
	c.delivering = false
	c.mu.Unlock()
}

// SetReceiver makes the connection hand each message to recv, in order, on
// a goroutine that received it, instead of queueing it for ReadMessage.
// Messages already queued go to recv first. recv must not block.
func (c *Conn) SetReceiver(recv func(msg []byte)) {
	c.mu.Lock()
	c.receiver = recv
	if recv != nil {
		for {
			msg, ok := c.dequeue()
			if !ok {
				break
			}
			c.pending = append(c.pending, msg)
		}
	}
	c.release()
}

// ReadMessage returns the next message received. Messages received before
// the connection closed are returned before the error.
func (c *Conn) ReadMessage() ([]byte, error) {
	var expired <-chan time.Time
	for {
		c.mu.Lock()
		msg, ok := c.dequeue()
		err, deadline := c.err, c.readDeadline
		c.mu.Unlock()
		switch {
		case ok:
			return msg, nil
		case err != nil:
			return nil, err
		case expired == nil && !deadline.IsZero():
			t := time.NewTimer(time.Until(deadline))
			defer t.Stop()
			expired = t.C
		}
		select {
		case <-c.readable:
		case <-c.closed:
		case <-expired:
			return nil, os.ErrDeadlineExceeded
		}
	}
}

// ReadPacket returns the next message received: a packet batch.
func (c *Conn) ReadPacket() ([]byte, error) { return c.ReadMessage() }

// Read reads the next message received into b. A message longer than b is
// cut short with io.ErrShortBuffer.
func (c *Conn) Read(b []byte) (int, error) {
	msg, err := c.ReadMessage()
	if err != nil {
		return 0, err
	}
	if len(msg) > len(b) {
		return copy(b, msg), io.ErrShortBuffer
	}
	return copy(b, msg), nil
}

// Send sends a message on the reliable channel. It does not block: the
// message is queued, or ErrBufferFull is returned if the peer did not take
// enough of what was sent before.
func (c *Conn) Send(msg []byte) error {
	c.mu.Lock()
	defer c.release()
	if c.err != nil {
		return c.err
	}
	if !c.ready || !c.assoc.Established() {
		return errNotOpen
	}
	if len(msg) == 0 {
		return nil
	}
	n := (len(msg) + c.maxSegment - 1) / c.maxSegment
	if n > maxSegments {
		return ErrTooLarge
	}
	if len(msg)+n > c.assoc.Available() {
		return ErrBufferFull
	}
	now := c.now()
	for i := range n {
		part := msg[i*c.maxSegment : min(len(msg), (i+1)*c.maxSegment)]
		segment := make([]byte, 1+len(part))
		segment[0] = byte(n - 1 - i)
		copy(segment[1:], part)
		if err := c.assoc.SendOwned(c.reliable.stream, sctp.PPIDBinary, segment, sctp.SendOptions{}, now); err != nil {
			// A message sent in part cannot be taken back.
			c.fail(err)
			return err
		}
	}
	c.schedule()
	return nil
}

// SendUnreliable sends a message on the unreliable channel: unordered and
// never sent again. A message that does not fit one segment is dropped.
func (c *Conn) SendUnreliable(msg []byte) error {
	c.mu.Lock()
	defer c.release()
	if c.err != nil {
		return c.err
	}
	if !c.ready || !c.unreliable.open || !c.assoc.Established() {
		return errNotOpen
	}
	if len(msg) == 0 || len(msg) > c.maxSegment {
		return nil
	}
	segment := make([]byte, 1+len(msg))
	copy(segment[1:], msg)
	err := c.assoc.SendOwned(c.unreliable.stream, sctp.PPIDBinary, segment, sctp.SendOptions{Unordered: true, Partial: true}, c.now())
	c.schedule()
	return err
}

// Write sends b as one message on the reliable channel. A peer that does
// not take what is sent closes the connection.
func (c *Conn) Write(b []byte) (int, error) {
	if err := c.Send(b); err != nil {
		if errors.Is(err, ErrBufferFull) {
			c.abort(errSlowPeer)
		}
		return 0, err
	}
	return len(b), nil
}

// BatchHeader returns nil: NetherNet messages carry packet batches without
// the batch header of RakNet.
func (c *Conn) BatchHeader() []byte { return nil }

// DisableEncryption reports that Minecraft encryption is off: DTLS
// encrypts the connection.
func (c *Conn) DisableEncryption() bool { return true }

// PublicKey returns the key the peer proved in its identity, or nil for an
// anonymous peer. The Login of a client must carry the same key.
func (c *Conn) PublicKey() *ecdsa.PublicKey { return c.publicKey }

// RTT returns the round trip time SCTP measured.
func (c *Conn) RTT() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.assoc.RTT()
}

// Latency returns half the round trip time.
func (c *Conn) Latency() time.Duration { return c.RTT() / 2 }

// Available returns how many bytes of messages Send takes now.
func (c *Conn) Available() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.assoc.Available()
}

// RemoteAddr returns the address of the peer.
func (c *Conn) RemoteAddr() net.Addr {
	c.mu.Lock()
	defer c.mu.Unlock()
	return net.UDPAddrFromAddrPort(c.remote)
}

// LocalAddr returns the address of the socket of the connection.
func (c *Conn) LocalAddr() net.Addr { return c.localAddr }

// SetDeadline sets the read deadline: writes never block.
func (c *Conn) SetDeadline(t time.Time) error { return c.SetReadDeadline(t) }

// SetReadDeadline sets the deadline of ReadMessage calls started after it.
func (c *Conn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	c.readDeadline = t
	c.mu.Unlock()
	return nil
}

// SetWriteDeadline does nothing: writes never block.
func (c *Conn) SetWriteDeadline(time.Time) error { return nil }

// Done returns a channel closed when the connection closes.
func (c *Conn) Done() <-chan struct{} { return c.closed }

// Err returns why the connection closed, or nil while it is open.
func (c *Conn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Close closes the connection. Messages sent before are still delivered
// for a few seconds.
func (c *Conn) Close() error {
	c.mu.Lock()
	defer c.release()
	c.shut(net.ErrClosed, true)
	return nil
}

// abort closes the connection at once.
func (c *Conn) abort(err error) {
	c.mu.Lock()
	defer c.release()
	c.shut(err, false)
}

func (c *Conn) fail(err error) { c.shut(err, false) }

// shut closes the connection for its user with err. A graceful close
// lets SCTP deliver what was sent before the transport goes.
func (c *Conn) shut(err error, graceful bool) {
	if c.err == nil {
		c.err = err
		close(c.closed)
		c.pending = nil
	}
	if c.finished {
		return
	}
	if graceful && c.linger.IsZero() && c.associated && c.assoc.Err() == nil {
		// Shutdown starts the exchange, or lets one the peer started run.
		now := c.now()
		c.linger = now.Add(lingerTimeout)
		c.assoc.Shutdown(now)
	}
	if graceful && !c.linger.IsZero() && c.assoc.Err() == nil {
		c.schedule()
		return
	}
	c.finish()
}

// finish ends the transport of the connection.
func (c *Conn) finish() {
	c.finished = true
	if c.assoc.Err() == nil {
		c.assoc.Abort()
	}
	c.dtls.Close()
	// The socket may close with the connection.
	c.flushBatch()
	if c.timer != nil {
		c.timer.Stop()
	}
	c.segments = nil
	if c.onClose != nil {
		c.onClose(c)
	}
}

// handleTimeout runs the timers of the layers that are due.
func (c *Conn) handleTimeout(now time.Time) {
	if c.finished {
		return
	}
	c.at = now
	if !c.expires.IsZero() && !now.Before(c.expires) {
		c.fail(errOpenTimeout)
		return
	}
	if !c.linger.IsZero() && !now.Before(c.linger) {
		c.finish()
		return
	}
	if err := c.agent.HandleTimeout(now); err != nil {
		c.fail(err)
		return
	}
	if d := c.dtls.Deadline(); !d.IsZero() && !now.Before(d) {
		if err := c.dtls.HandleTimeout(now); err != nil {
			c.fail(err)
			return
		}
	}
	if d := c.assoc.Deadline(); !d.IsZero() && !now.Before(d) {
		if err := c.assoc.HandleTimeout(now); err != nil {
			c.associationDone(err)
			return
		}
	}
	c.progress(now)
}

// nextDeadline returns the earliest timer of the layers.
func (c *Conn) nextDeadline() time.Time {
	if c.finished {
		return time.Time{}
	}
	d := c.agent.Deadline()
	for _, t := range [...]time.Time{c.dtls.Deadline(), c.assoc.Deadline(), c.expires, c.linger} {
		if !t.IsZero() && (d.IsZero() || t.Before(d)) {
			d = t
		}
	}
	return d
}

// schedule arms the timer of the connection for its next deadline. A timer
// armed for later than needed is moved; one armed earlier fires and arms
// itself again.
func (c *Conn) schedule() {
	if c.clock != nil || c.finished {
		return
	}
	next := c.nextDeadline()
	if next.IsZero() || !c.armed.IsZero() && !next.Before(c.armed) {
		return
	}
	c.armed = next
	if c.timer == nil {
		c.timer = time.AfterFunc(time.Until(next), c.fire)
	} else {
		c.timer.Reset(time.Until(next))
	}
}

func (c *Conn) fire() {
	c.mu.Lock()
	defer c.release()
	c.armed = time.Time{}
	c.handleTimeout(time.Now())
	c.schedule()
}
