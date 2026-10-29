package lan

import (
	"encoding/binary"
	"errors"
)

// Game types of ServerData.
const (
	GameTypeSurvival int32 = iota
	GameTypeCreative
	GameTypeAdventure
)

// ConnectionTypeLAN is the ConnectionType of a game joined over NetherNet
// on the network.
const ConnectionTypeLAN = 4

// dataVersion is the version of the format of ServerData.
const dataVersion = 7

// ServerData is what the LAN tab of a client shows of a server.
type ServerData struct {
	ServerName                               string
	Protocol                                 int32
	Version                                  string
	LevelName                                string
	GameType                                 int32
	PlayerCount, MaxPlayerCount              int32
	EditorWorld, Hardcore                    bool
	AcceptsOnlineAuth, AcceptsSelfSignedAuth bool
	Nonce                                    string
	ConnectionType                           int32
}

// MarshalBinary encodes the data as a Response carries it.
func (d ServerData) MarshalBinary() ([]byte, error) {
	b := []byte{dataVersion}
	b = appendString(b, d.ServerName)
	b = binary.AppendUvarint(b, zigzag(d.Protocol))
	b = appendString(b, d.Version)
	b = appendString(b, d.LevelName)
	b = binary.AppendUvarint(b, zigzag(d.PlayerCount))
	b = binary.AppendUvarint(b, zigzag(d.MaxPlayerCount))
	b = binary.AppendUvarint(b, zigzag(d.GameType))
	b = append(b, boolByte(d.EditorWorld), boolByte(d.Hardcore), boolByte(d.AcceptsOnlineAuth), boolByte(d.AcceptsSelfSignedAuth))
	b = appendString(b, d.Nonce)
	return binary.AppendUvarint(b, zigzag(d.ConnectionType)), nil
}

var errData = errors.New("lan: malformed server data")

// UnmarshalBinary decodes data a Response carried.
func (d *ServerData) UnmarshalBinary(b []byte) error {
	r := reader{b: b}
	if r.byte() != dataVersion {
		return errData
	}
	d.ServerName = r.string()
	d.Protocol = r.varint32()
	d.Version = r.string()
	d.LevelName = r.string()
	d.PlayerCount = r.varint32()
	d.MaxPlayerCount = r.varint32()
	d.GameType = r.varint32()
	d.EditorWorld, d.Hardcore = r.byte() != 0, r.byte() != 0
	d.AcceptsOnlineAuth, d.AcceptsSelfSignedAuth = r.byte() != 0, r.byte() != 0
	d.Nonce = r.string()
	d.ConnectionType = r.varint32()
	if r.bad || len(r.b) != 0 {
		return errData
	}
	return nil
}

func appendString(b []byte, s string) []byte {
	return append(binary.AppendUvarint(b, uint64(len(s))), s...)
}

func zigzag(v int32) uint64 { return uint64(uint32(v<<1) ^ uint32(v>>31)) }

func boolByte(v bool) byte {
	if v {
		return 1
	}
	return 0
}

// reader reads the fields of ServerData, going bad at the first one that
// does not fit.
type reader struct {
	b   []byte
	bad bool
}

func (r *reader) byte() byte {
	if len(r.b) == 0 {
		r.bad = true
		return 0
	}
	v := r.b[0]
	r.b = r.b[1:]
	return v
}

func (r *reader) uvarint32() uint32 {
	v, n := binary.Uvarint(r.b)
	if n <= 0 || n > 5 || v > 1<<32-1 {
		r.bad, r.b = true, nil
		return 0
	}
	r.b = r.b[n:]
	return uint32(v)
}

func (r *reader) varint32() int32 {
	u := r.uvarint32()
	return int32(u>>1) ^ -int32(u&1)
}

func (r *reader) string() string {
	n := r.uvarint32()
	if uint64(n) > uint64(len(r.b)) {
		r.bad, r.b = true, nil
		return ""
	}
	s := string(r.b[:n])
	r.b = r.b[n:]
	return s
}
