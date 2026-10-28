package bedrock

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
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
	return cfg
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
	}
	p := c.pending[0]
	c.pending[0] = nil
	c.pending = c.pending[1:]
	if len(c.pending) == 0 {
		c.pending = nil
	}
	return p, nil
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
