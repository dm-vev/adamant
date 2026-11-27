package bedrock

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"
)

// MessageConn is the transport under a Conn: whole messages each way, in
// order and reliably, such as a nethernet.Conn.
type MessageConn interface {
	// ReadMessage returns the next message received, owned by the caller.
	ReadMessage() ([]byte, error)
	// Send sends a message. It may keep no reference to it.
	Send(msg []byte) error
	Close() error
}

// Config configures a Conn.
type Config struct {
	// Threshold is the smallest body of a batch that is compressed; smaller
	// ones go as they are. Zero means 256 bytes.
	Threshold int
	// Incompressible reports packets that compress too little to be worth
	// the CPU, such as movement: a batch of only such packets goes as it
	// is. Nil means none.
	Incompressible func(id uint32) bool
	// MaxBody is the size past which the packets written are sent before
	// another is added, so that a peer can act on part of a large burst
	// while the rest arrives. Zero means 64 KiB.
	MaxBody int
	// MaxBatch bounds a batch received once decompressed. Zero means 8 MiB.
	MaxBatch int
	// MaxPackets bounds the packets of a batch received. Zero means 1024.
	MaxPackets int
	// PacketRate and ByteRate bound the packets and the decompressed bytes
	// received a second, PacketBurst and ByteBurst how far a peer may run
	// ahead of them; a peer past either fails the read with
	// ErrRateLimited. A zero rate means no bound; a burst under the largest
	// batch allowed is raised to it.
	PacketRate, PacketBurst int
	ByteRate, ByteBurst     int
	// Cache, if set, is shared by connections whose batches often go
	// alike, which it compresses once. Nil compresses each batch.
	Cache *BatchCache
}

func (cfg Config) withDefaults() Config {
	if cfg.Threshold == 0 {
		cfg.Threshold = 256
	}
	if cfg.MaxBody == 0 {
		cfg.MaxBody = 64 << 10
	}
	if cfg.MaxBatch == 0 {
		cfg.MaxBatch = 8 << 20
	}
	if cfg.MaxPackets == 0 {
		cfg.MaxPackets = 1024
	}
	cfg.PacketBurst = max(cfg.PacketBurst, cfg.MaxPackets)
	cfg.ByteBurst = max(cfg.ByteBurst, cfg.MaxBatch)
	return cfg
}

// ErrRateLimited is returned once a peer sends more than the rates of the
// Config allow.
var ErrRateLimited = errors.New("bedrock: peer sends too much")

// bucket holds tokens that come back at a rate up to a burst.
type bucket struct {
	tokens float64
	last   time.Time
}

// take takes n tokens, reporting false once there were too few.
func (b *bucket) take(now time.Time, n, rate, burst int) bool {
	if b.last.IsZero() {
		b.tokens = float64(burst)
	} else {
		b.tokens = min(float64(burst), b.tokens+now.Sub(b.last).Seconds()*float64(rate))
	}
	b.last = now
	b.tokens -= float64(n)
	return b.tokens >= 0
}

var errAlgorithm = errors.New("bedrock: batch names an unknown compression algorithm")

// keptBuffer bounds the capacity of the buffers a Conn keeps between
// batches: the batch of a tick fits, the rare large ones are not kept for
// the life of the connection.
const keptBuffer = 64 << 10

// noneByte is the first byte of a batch sent as it is.
const noneByte = byte(AlgorithmNone & 0xff)

// segmentMinBytes is the minimum segment length between recorded boundaries so
// sync-flush stored block headers never inflate small packet runs.
const segmentMinBytes = 256

// Conn reads and writes the packets of a Bedrock connection: a packet is
// its header and fields, as a codec encodes them. Packets written gather in
// a batch until Flush; packets are read a batch at a time. Its methods are
// safe for concurrent use and it has no goroutine of its own.
type Conn struct {
	transport MessageConn
	cfg       Config

	readMu      sync.Mutex
	readAlg     Compression
	pending     [][]byte
	pendingHead int
	// split holds the packets of the batch HandleBatch reads.
	split [][]byte
	// recv counts the packets of a batch read before adding to the process counters.
	recv tally
	// packets and bytes meter what the peer sends against the rates.
	inPackets, inBytes bucket

	writeMu     sync.Mutex
	batchWriter BatchWriter
	alg         Compression
	body        []byte
	segments    []int
	// plain reports whether every packet of body is incompressible.
	plain bool
	out   []byte
	// sent counts the packets of body for the metrics.
	sent tally
}

// BatchWriter writes a sequence of packets, pre-framed payloads, and segment
// marks to a Conn while holding its write lock once.
type BatchWriter struct {
	c *Conn
}

// WriteFramed appends an already length-prefixed packet to the current batch.
func (w *BatchWriter) WriteFramed(framed []byte, id uint32, rawLen int) error {
	if len(framed) == 0 || rawLen <= 0 {
		return errEmptyPacket
	}
	return w.c.writeFramed(framed, id, rawLen)
}

// WritePacket appends a raw packet (header and fields) to the current batch.
func (w *BatchWriter) WritePacket(raw []byte) error {
	if len(raw) == 0 {
		return errEmptyPacket
	}
	id, ok := packetID(raw)
	return w.c.writePacket(raw, id, ok)
}

// MarkSegment records a segment boundary in the current batch if at least
// segmentMinBytes have been buffered since the last mark.
func (w *BatchWriter) MarkSegment() {
	w.c.markSegment()
}

// SendBatch sends a pre-encoded batch after flushing any buffered packets.
func (w *BatchWriter) SendBatch(msg []byte) error {
	return w.c.sendBatch(msg)
}

// Flush sends the packets gathered so far as one batch.
func (w *BatchWriter) Flush() error {
	return w.c.flush()
}

// WriteBatch invokes fn with a BatchWriter while holding the connection's write
// mutex once.
func (c *Conn) WriteBatch(fn func(*BatchWriter) error) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.batchWriter.c = c
	return fn(&c.batchWriter)
}

// NewConn returns a Conn over a transport. Compression is off until
// EnableCompression, as before NetworkSettings.
func NewConn(transport MessageConn, cfg Config) *Conn {
	return &Conn{transport: transport, cfg: cfg.withDefaults(), plain: true}
}

// EnableCompression turns compression on in both directions: every batch
// from now on starts with the byte of its algorithm, and the batches sent
// above the threshold use c.
func (c *Conn) EnableCompression(comp Compression) {
	c.writeMu.Lock()
	c.alg = comp
	c.writeMu.Unlock()
	c.readMu.Lock()
	c.readAlg = comp
	c.readMu.Unlock()
}

// WritePacket adds a packet to the batch being gathered. The packet is
// copied.
func (c *Conn) WritePacket(packet []byte) error {
	if len(packet) == 0 {
		return errEmptyPacket
	}
	id, ok := packetID(packet)
	c.writeMu.Lock()
	err := c.writePacket(packet, id, ok)
	c.writeMu.Unlock()
	return err
}

func (c *Conn) writePacket(packet []byte, id uint16, ok bool) error {
	if len(c.body) > 0 && len(c.body)+len(packet) > c.cfg.MaxBody {
		if err := c.flush(); err != nil {
			return err
		}
	}
	c.markPlain(uint32(id))
	c.body = AppendPacket(c.body, packet)
	if ok {
		c.sent.addID(packetsSent, packetBytesSent, id, uint64(len(packet)))
	}
	return nil
}

func (c *Conn) writeFramed(framed []byte, id uint32, rawLen int) error {
	if len(c.body) > 0 && len(c.body)+len(framed) > c.cfg.MaxBody {
		if err := c.flush(); err != nil {
			return err
		}
	}
	c.markPlain(id)
	c.body = append(c.body, framed...)
	c.sent.addID(packetsSent, packetBytesSent, uint16(id&(maxPacketID-1)), uint64(rawLen))
	return nil
}

func (c *Conn) markPlain(id uint32) {
	if c.plain && c.cfg.Incompressible != nil {
		c.plain = c.cfg.Incompressible(id)
		return
	}
	c.plain = false
}

func (c *Conn) markSegment() {
	last := 0
	if n := len(c.segments); n > 0 {
		last = c.segments[n-1]
	}
	if len(c.body)-last >= segmentMinBytes {
		c.segments = append(c.segments, len(c.body))
	}
}

// encodeBatchBufs pools temporary body buffers for EncodeBatch.
var encodeBatchBufs sync.Pool

// EncodeBatch returns a batch of packets as a Conn compressing with comp
// sends it, for SendBatch to send on many connections.
func EncodeBatch(comp Compression, packets ...[]byte) ([]byte, error) {
	total := 0
	for _, p := range packets {
		n := len(p)
		switch {
		case n < 0x80:
			total += 1 + n
		case n < 0x4000:
			total += 2 + n
		default:
			total += binary.MaxVarintLen64 + n
		}
	}
	var body []byte
	var bp *[]byte
	if total <= keptBuffer {
		bp, _ = encodeBatchBufs.Get().(*[]byte)
		if bp != nil {
			body = (*bp)[:0]
		}
	}
	if cap(body) < total {
		body = make([]byte, 0, total)
	}
	for _, p := range packets {
		body = AppendPacket(body, p)
	}
	out, err := comp.Compress([]byte{byte(comp.Algorithm())}, body)
	if cap(body) <= keptBuffer {
		if bp == nil {
			bp = new([]byte)
		}
		*bp = body[:0]
		encodeBatchBufs.Put(bp)
	}
	return out, err
}

// SendBatch sends a batch made by EncodeBatch with the compression of the
// Conn, after the packets gathered so far. The batch is not kept.
func (c *Conn) SendBatch(msg []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.sendBatch(msg)
}

func (c *Conn) sendBatch(msg []byte) error {
	if c.alg == nil || len(msg) == 0 || msg[0] != byte(c.alg.Algorithm()) && msg[0] != noneByte {
		return errors.New("bedrock: batch of another compression")
	}
	if err := c.flush(); err != nil {
		return err
	}
	batchesSent.Inc()
	batchBytesSent.Add(uint64(len(msg)))
	return c.transport.Send(msg)
}

// Flush sends the packets gathered as one batch.
func (c *Conn) Flush() error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.flush()
}

func (c *Conn) flush() error {
	if len(c.body) == 0 {
		return nil
	}
	msg := c.body
	if c.alg != nil {
		var err error
		if len(c.body) < c.cfg.Threshold || c.plain || c.alg.Algorithm() == AlgorithmNone {
			c.out = append(append(c.out[:0], noneByte), c.body...)
		} else if fl, ok := asFlate(c.alg); ok && c.cfg.Cache != nil && len(c.segments) >= 1 && allSegmentsBelow(c.segments, len(c.body), fl.fixedBelow()) {
			if err = c.flushSegmented(fl); err != nil {
				return err
			}
		} else if isSnappy(c.alg) && c.cfg.Cache != nil && len(c.segments) >= 1 {
			c.flushSegmentedSnappy()
		} else if c.cfg.Cache != nil {
			if c.out, err = c.cfg.Cache.compress(c.out[:0], c.alg, c.body); err != nil {
				return err
			}
		} else if c.out, err = c.alg.Compress(append(c.out[:0], byte(c.alg.Algorithm())), c.body); err != nil {
			return err
		}
		msg = c.out
	}
	batchesSent.Inc()
	batchBytesRaw.Add(uint64(len(c.body)))
	batchBytesSent.Add(uint64(len(msg)))
	c.sent.flush(packetsSent, packetBytesSent)
	err := c.transport.Send(msg)
	c.body, c.plain, c.segments = c.body[:0], true, c.segments[:0]
	if cap(c.body) > keptBuffer {
		c.body = nil
	}
	if cap(c.out) > keptBuffer {
		c.out = nil
	}
	if cap(c.segments) > 256 {
		c.segments = nil
	}
	return err
}

func (c *Conn) flushSegmented(fl Flate) error {
	bc := c.cfg.Cache
	if out, ok := bc.lookup(c.out[:0], AlgorithmFlate, c.body); ok {
		c.out = out
		return nil
	}
	out := c.out[:0]
	out = append(out, byte(AlgorithmFlate))
	prev := 0
	var err error
	for _, end := range c.segments {
		if out, err = bc.compressSegment(out, fl, c.body[prev:end]); err != nil {
			return err
		}
		prev = end
	}
	if prev < len(c.body) {
		if out, err = bc.compressSegment(out, fl, c.body[prev:]); err != nil {
			return err
		}
	}
	out = append(out, deflateFinalEmpty[:]...)
	c.out = out
	return nil
}

func (c *Conn) flushSegmentedSnappy() {
	bc := c.cfg.Cache
	if out, ok := bc.lookup(c.out[:0], AlgorithmSnappy, c.body); ok {
		c.out = out
		return
	}
	out := append(c.out[:0], byte(AlgorithmSnappy))
	out = binary.AppendUvarint(out, uint64(len(c.body)))
	prev := 0
	for _, end := range c.segments {
		out = bc.compressSnappySegment(out, c.body[prev:end])
		prev = end
	}
	if prev < len(c.body) {
		out = bc.compressSnappySegment(out, c.body[prev:])
	}
	c.out = out
}

func asFlate(comp Compression) (Flate, bool) {
	switch c := comp.(type) {
	case Flate:
		return c, true
	case *Flate:
		if c != nil {
			return *c, true
		}
	}
	return Flate{}, false
}

func isSnappy(comp Compression) bool {
	switch c := comp.(type) {
	case Snappy:
		return true
	case *Snappy:
		return c != nil
	}
	return false
}

func allSegmentsBelow(segments []int, bodyLen, limit int) bool {
	if limit <= 0 {
		return false
	}
	prev := 0
	for _, end := range segments {
		if end-prev >= limit {
			return false
		}
		prev = end
	}
	return bodyLen-prev < limit
}

// ReadPacket returns the next packet received. It is the caller's to keep.
func (c *Conn) ReadPacket() ([]byte, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	for c.pendingHead >= len(c.pending) {
		c.pending, c.pendingHead = c.pending[:0], 0
		msg, err := c.transport.ReadMessage()
		if err != nil {
			return nil, err
		}
		body, err := c.decode(msg)
		if err != nil {
			return nil, err
		}
		// The packets share the body of their batch, which no one reuses.
		err = Packets(body, c.cfg.MaxPackets, func(p []byte) error {
			c.pending = append(c.pending, p[:len(p):len(p)])
			c.recv.add(packetsReceived, packetBytesReceived, p)
			return nil
		})
		c.recv.flush(packetsReceived, packetBytesReceived)
		if err != nil {
			clear(c.pending)
			c.pending = c.pending[:0]
			return nil, err
		}
		if err := c.meter(len(c.pending), len(body)); err != nil {
			clear(c.pending)
			c.pending = c.pending[:0]
			return nil, err
		}
	}
	p := c.pending[c.pendingHead]
	c.pending[c.pendingHead] = nil
	c.pendingHead++
	if c.pendingHead == len(c.pending) {
		c.pendingHead = 0
		if cap(c.pending) > 64 {
			c.pending = nil
		} else {
			c.pending = c.pending[:0]
		}
	}
	return p, nil
}

// HandleBatch reads a batch the transport handed over other than through
// ReadMessage, as it does to a receiver of its messages, and calls f with
// each packet of it in order, which is f's to keep. It keeps the limits
// and the rates of ReadPacket. An error of f stops it and is returned.
func (c *Conn) HandleBatch(msg []byte, f func(packet []byte) error) error {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	body, err := c.decode(msg)
	if err != nil {
		return err
	}
	split := c.split[:0]
	defer func() {
		clear(split)
		if cap(split) > 64 {
			c.split = nil
		} else {
			c.split = split[:0]
		}
	}()
	err = Packets(body, c.cfg.MaxPackets, func(p []byte) error {
		split = append(split, p[:len(p):len(p)])
		c.recv.add(packetsReceived, packetBytesReceived, p)
		return nil
	})
	c.recv.flush(packetsReceived, packetBytesReceived)
	if err != nil {
		return err
	}
	if err := c.meter(len(split), len(body)); err != nil {
		return err
	}
	for _, p := range split {
		if err := f(p); err != nil {
			return err
		}
	}
	return nil
}

// TakePending returns the packets of the batches ReadPacket read that it
// did not return yet, which are the caller's to keep.
func (c *Conn) TakePending() [][]byte {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	if c.pendingHead >= len(c.pending) {
		c.pending, c.pendingHead = nil, 0
		return nil
	}
	p := c.pending[c.pendingHead:]
	c.pending, c.pendingHead = nil, 0
	return p
}

// meter takes a batch of n packets and size bytes from the rates.
func (c *Conn) meter(n, size int) error {
	if c.cfg.PacketRate == 0 && c.cfg.ByteRate == 0 {
		return nil
	}
	now := time.Now()
	if c.cfg.PacketRate > 0 && !c.inPackets.take(now, n, c.cfg.PacketRate, c.cfg.PacketBurst) ||
		c.cfg.ByteRate > 0 && !c.inBytes.take(now, size, c.cfg.ByteRate, c.cfg.ByteBurst) {
		rateLimited.Inc()
		return ErrRateLimited
	}
	return nil
}

// decode returns the body of a batch received.
func (c *Conn) decode(msg []byte) ([]byte, error) {
	if c.readAlg == nil {
		if len(msg) > c.cfg.MaxBatch {
			return nil, ErrTooLarge
		}
		return msg, nil
	}
	if len(msg) == 0 {
		return nil, fmt.Errorf("bedrock: empty batch")
	}
	switch uint16(msg[0]) {
	case uint16(noneByte):
		if len(msg)-1 > c.cfg.MaxBatch {
			return nil, ErrTooLarge
		}
		return msg[1:], nil
	case c.readAlg.Algorithm():
		return c.readAlg.Decompress(nil, msg[1:], c.cfg.MaxBatch)
	case AlgorithmFlate:
		return Flate{}.Decompress(nil, msg[1:], c.cfg.MaxBatch)
	}
	return nil, errAlgorithm
}

// Close closes the transport.
func (c *Conn) Close() error { return c.transport.Close() }
