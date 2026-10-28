package sctp

import (
	"encoding/binary"
	"errors"
	"math"
	"time"
)

// Payload protocol identifiers of WebRTC data channels (RFC 8831).
const (
	PPIDControl     = 50
	PPIDString      = 51
	PPIDBinary      = 53
	PPIDStringEmpty = 56
	PPIDBinaryEmpty = 57
)

// Data channel types of RFC 8832.
const (
	ChannelReliable                    = 0x00
	ChannelReliableUnordered           = 0x80
	ChannelPartialRexmit               = 0x01
	ChannelPartialRexmitUnordered      = 0x81
	ChannelPartialTimed                = 0x02
	ChannelPartialTimedUnordered       = 0x82
	messageDataChannelAck         byte = 0x02
	messageDataChannelOpen        byte = 0x03
)

// ChannelOpen is a DATA_CHANNEL_OPEN message.
type ChannelOpen struct {
	Type        byte
	Priority    uint16
	Reliability uint32
	Label       string
	Protocol    string
}

// Unordered reports whether the channel delivers messages out of order.
func (o ChannelOpen) Unordered() bool { return o.Type&0x80 != 0 }

// SendOptions returns the options of the messages sent on the channel.
func (o ChannelOpen) SendOptions() SendOptions {
	opts := SendOptions{Unordered: o.Unordered()}
	switch o.Type & 0x7f {
	case ChannelPartialRexmit:
		opts.Partial, opts.MaxRetransmits = true, int(o.Reliability)
	case ChannelPartialTimed:
		opts.Partial, opts.MaxRetransmits = true, math.MaxInt
		opts.Lifetime = time.Duration(o.Reliability) * time.Millisecond
	}
	return opts
}

var errChannelMessage = errors.New("sctp: malformed data channel message")

// ParseChannelOpen decodes a DATA_CHANNEL_OPEN message.
func ParseChannelOpen(b []byte) (ChannelOpen, error) {
	if len(b) < 12 || b[0] != messageDataChannelOpen {
		return ChannelOpen{}, errChannelMessage
	}
	labelLen := int(binary.BigEndian.Uint16(b[8:]))
	protocolLen := int(binary.BigEndian.Uint16(b[10:]))
	if len(b) != 12+labelLen+protocolLen {
		return ChannelOpen{}, errChannelMessage
	}
	return ChannelOpen{
		Type:        b[1],
		Priority:    binary.BigEndian.Uint16(b[2:]),
		Reliability: binary.BigEndian.Uint32(b[4:]),
		Label:       string(b[12 : 12+labelLen]),
		Protocol:    string(b[12+labelLen:]),
	}, nil
}

// Marshal encodes a DATA_CHANNEL_OPEN message.
func (o ChannelOpen) Marshal() []byte {
	b := []byte{messageDataChannelOpen, o.Type}
	b = binary.BigEndian.AppendUint16(b, o.Priority)
	b = binary.BigEndian.AppendUint32(b, o.Reliability)
	b = binary.BigEndian.AppendUint16(b, uint16(len(o.Label)))
	b = binary.BigEndian.AppendUint16(b, uint16(len(o.Protocol)))
	b = append(b, o.Label...)
	return append(b, o.Protocol...)
}

// ChannelAck returns a DATA_CHANNEL_ACK message.
func ChannelAck() []byte { return []byte{messageDataChannelAck} }

// IsChannelAck reports whether b is a DATA_CHANNEL_ACK message.
func IsChannelAck(b []byte) bool { return len(b) == 1 && b[0] == messageDataChannelAck }
