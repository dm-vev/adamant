// Package lan publishes NetherNet games to the LAN tab of clients on the
// same network and carries their signaling there, as the game does on UDP
// port 7551: a client broadcasts a request, servers answer with what the
// tab shows, and the signals of a join go in messages between the network
// IDs of the two. Every packet is encrypted with AES-ECB under a key all
// games share and signed with HMAC-SHA256 under it: the scheme hides
// nothing from anyone on the network, it only keeps out what is not a
// packet of the game.
package lan

import (
	"bytes"
	"crypto/aes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
)

// Port is the UDP port LAN discovery runs on.
const Port = 7551

// key encrypts and signs every packet: SHA-256 of the application ID
// 0xdeadbeef as a little-endian uint64.
var key = sha256.Sum256(binary.LittleEndian.AppendUint64(nil, 0xdeadbeef))

// IDs of the packets.
const (
	idRequest uint16 = iota
	idResponse
	idMessage
)

// Packet is a packet of LAN discovery: *Request, *Response or *Message.
type Packet interface{ id() uint16 }

// Request asks the servers on the network for their data.
type Request struct{}

// Response answers a Request with the data of a server, ServerData
// marshalled.
type Response struct{ ApplicationData []byte }

// Message carries a signal to the network ID of its recipient.
type Message struct {
	RecipientID uint64
	Data        string
}

func (*Request) id() uint16  { return idRequest }
func (*Response) id() uint16 { return idResponse }
func (*Message) id() uint16  { return idMessage }

// maxPacket bounds a packet: a datagram, and the length its header names.
const maxPacket = 1<<16 - 1

var errPacket = errors.New("lan: malformed packet")

// Marshal encodes a packet of sender: its HMAC, then the payload encrypted.
// The payload is its length, the header of ID, sender and eight bytes of
// padding, and the fields of the packet.
func Marshal(pk Packet, sender uint64) []byte {
	payload := make([]byte, 2, 64)
	payload = binary.LittleEndian.AppendUint16(payload, pk.id())
	payload = binary.LittleEndian.AppendUint64(payload, sender)
	payload = append(payload, make([]byte, 8)...)
	switch pk := pk.(type) {
	case *Response:
		data := make([]byte, hex.EncodedLen(len(pk.ApplicationData)))
		hex.Encode(data, pk.ApplicationData)
		payload = binary.LittleEndian.AppendUint32(payload, uint32(len(data)))
		payload = append(payload, data...)
	case *Message:
		payload = binary.LittleEndian.AppendUint64(payload, pk.RecipientID)
		payload = binary.LittleEndian.AppendUint32(payload, uint32(len(pk.Data)))
		payload = append(payload, pk.Data...)
	}
	binary.LittleEndian.PutUint16(payload, uint16(len(payload)))
	mac := hmac.New(sha256.New, key[:])
	mac.Write(payload)
	return append(mac.Sum(nil), encrypt(payload)...)
}

// Unmarshal decodes a packet and returns it with its sender. Any datagram
// that is not a packet of the game whole and signed is an error.
func Unmarshal(b []byte) (Packet, uint64, error) {
	if len(b) < sha256.Size+aes.BlockSize || len(b) > maxPacket {
		return nil, 0, errPacket
	}
	payload, err := decrypt(b[sha256.Size:])
	if err != nil {
		return nil, 0, err
	}
	mac := hmac.New(sha256.New, key[:])
	mac.Write(payload)
	if !hmac.Equal(mac.Sum(nil), b[:sha256.Size]) {
		return nil, 0, errors.New("lan: packet does not match its HMAC")
	}
	// Length, ID, sender and padding. The game does not rely on the length,
	// so neither does this.
	if len(payload) < 20 {
		return nil, 0, errPacket
	}
	sender := binary.LittleEndian.Uint64(payload[4:])
	fields := payload[20:]
	switch binary.LittleEndian.Uint16(payload[2:]) {
	case idRequest:
		if len(fields) != 0 {
			return nil, 0, errPacket
		}
		return &Request{}, sender, nil
	case idResponse:
		data, rest, ok := lengthPrefixed(fields)
		if !ok || len(rest) != 0 || len(data)%2 != 0 {
			return nil, 0, errPacket
		}
		app := make([]byte, len(data)/2)
		if _, err := hex.Decode(app, data); err != nil {
			return nil, 0, errPacket
		}
		return &Response{ApplicationData: app}, sender, nil
	case idMessage:
		if len(fields) < 8 {
			return nil, 0, errPacket
		}
		data, rest, ok := lengthPrefixed(fields[8:])
		if !ok {
			return nil, 0, errPacket
		}
		// What follows the data belongs to it, as go-nethernet reads it.
		return &Message{RecipientID: binary.LittleEndian.Uint64(fields), Data: string(data) + string(rest)}, sender, nil
	}
	return nil, 0, fmt.Errorf("lan: unknown packet %d", binary.LittleEndian.Uint16(payload[2:]))
}

// lengthPrefixed splits off bytes after their little-endian uint32 length.
func lengthPrefixed(b []byte) (data, rest []byte, ok bool) {
	if len(b) < 4 {
		return nil, nil, false
	}
	n := binary.LittleEndian.Uint32(b)
	if uint64(n) > uint64(len(b)-4) {
		return nil, nil, false
	}
	return b[4 : 4+n], b[4+n:], true
}

// encrypt pads b as PKCS #7 does and encrypts it block by block.
func encrypt(b []byte) []byte {
	block, _ := aes.NewCipher(key[:])
	pad := aes.BlockSize - len(b)%aes.BlockSize
	out := append(bytes.Clone(b), bytes.Repeat([]byte{byte(pad)}, pad)...)
	for i := 0; i < len(out); i += aes.BlockSize {
		block.Encrypt(out[i:], out[i:])
	}
	return out
}

// decrypt decrypts b block by block and removes its PKCS #7 padding.
func decrypt(b []byte) ([]byte, error) {
	if len(b) == 0 || len(b)%aes.BlockSize != 0 {
		return nil, errPacket
	}
	block, _ := aes.NewCipher(key[:])
	out := make([]byte, len(b))
	for i := 0; i < len(b); i += aes.BlockSize {
		block.Decrypt(out[i:], b[i:])
	}
	pad := int(out[len(out)-1])
	if pad == 0 || pad > aes.BlockSize {
		return nil, errPacket
	}
	for _, p := range out[len(out)-pad:] {
		if int(p) != pad {
			return nil, errPacket
		}
	}
	return out[:len(out)-pad], nil
}
