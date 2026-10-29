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

// Conn reads and writes the packets of a Bedrock connection: a packet is
// its header and fields, as a codec encodes them. Packets written gather in
// a batch until Flush; packets are read a batch at a time. Its methods are
// safe for concurrent use and it has no goroutine of its own.
type Conn struct {
	transport MessageConn
	cfg       Config

	readMu  sync.Mutex
	readAlg Compression
	pending [][]byte
	// split holds the packets of the batch HandleBatch reads.
	split [][]byte
	// packets and bytes meter what the peer sends against the rates.
	inPackets, inBytes bucket

	writeMu sync.Mutex
	alg     Compression
	body    []byte
	// plain reports whether every packet of body is incompressible.
	plain bool
	out   []byte
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
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if len(c.body) > 0 && len(c.body)+len(packet) > c.cfg.MaxBody {
		if err := c.flush(); err != nil {
			return err
		}
	}
	if c.plain && c.cfg.Incompressible != nil {
		id, _ := binary.Uvarint(packet)
		c.plain = c.cfg.Incompressible(uint32(id) & 0x3ff)
	} else {
		c.plain = false
	}
	c.body = AppendPacket(c.body, packet)
	return nil
}

// EncodeBatch returns a batch of packets as a Conn compressing with comp
// sends it, for SendBatch to send on many connections.
func EncodeBatch(comp Compression, packets ...[]byte) ([]byte, error) {
	var body []byte
	for _, p := range packets {
		body = AppendPacket(body, p)
	}
	return comp.Compress([]byte{byte(comp.Algorithm())}, body)
}

// SendBatch sends a batch made by EncodeBatch with the compression of the
// Conn, after the packets gathered so far. The batch is not kept.
func (c *Conn) SendBatch(msg []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.alg == nil || len(msg) == 0 || msg[0] != byte(c.alg.Algorithm()) && msg[0] != noneByte {
		return errors.New("bedrock: batch of another compression")
	}
	if err := c.flush(); err != nil {
		return err
	}
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
		} else if c.out, err = c.alg.Compress(append(c.out[:0], byte(c.alg.Algorithm())), c.body); err != nil {
			return err
		}
		msg = c.out
	}
	err := c.transport.Send(msg)
	c.body, c.plain = c.body[:0], true
	if cap(c.body) > keptBuffer {
		c.body = nil
	}
	if cap(c.out) > keptBuffer {
		c.out = nil
	}
	return err
}

// ReadPacket returns the next packet received. It is the caller's to keep.
func (c *Conn) ReadPacket() ([]byte, error) {
	c.readMu.Lock()
	defer c.readMu.Unlock()
	for len(c.pending) == 0 {
		msg, err := c.transport.ReadMessage()
		if err != nil {
			return nil, err
		}
		body, err := c.decode(msg)
		if err != nil {
			return nil, err
		}
		// The packets share the body of their batch, which no one reuses.
		if err := Packets(body, c.cfg.MaxPackets, func(p []byte) error {
			c.pending = append(c.pending, p[:len(p):len(p)])
			return nil
		}); err != nil {
			c.pending = c.pending[:0]
			return nil, err
		}
		if err := c.meter(len(c.pending), len(body)); err != nil {
			c.pending = c.pending[:0]
			return nil, err
		}
	}
	p := c.pending[0]
	c.pending[0] = nil
	c.pending = c.pending[1:]
	if len(c.pending) == 0 {
		c.pending = nil
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
		c.split = split[:0]
	}()
	if err := Packets(body, c.cfg.MaxPackets, func(p []byte) error {
		split = append(split, p[:len(p):len(p)])
		return nil
	}); err != nil {
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
	p := c.pending
	c.pending = nil
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
