package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dm-vev/adamant/net/bedrock/client"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// capture is what -capture writes: what the server sends of the fields 2225
// added, and the blocks the commands of -commands placed, as the network
// block hashes the server names their states by in the block updates and
// the palettes of the sub-chunks around the player it sends after them.
type capture struct {
	Protocol int32 `json:"protocol"`
	// Origin is the block the player stood in, as the server last moved
	// it or as StartGame placed it, around which the sub-chunks are asked
	// for.
	Origin [3]int32 `json:"origin"`
	// Outputs are the outputs of the commands, in order.
	Outputs []string `json:"outputs"`
	// EditorLevelMigrationVersion is that of StartGame.
	EditorLevelMigrationVersion int8 `json:"editor_level_migration_version"`
	// ClientBiomeUpdate counts the LevelChunk packets by their flag.
	ClientBiomeUpdate map[string]int `json:"client_biome_update"`
	// Dimensions are those of DimensionData.
	Dimensions []mc.DimensionDefinition `json:"dimensions,omitempty"`
	Commands   []string                 `json:"commands"`
	Updates    []capturedUpdate         `json:"updates"`
	SubChunks  []capturedSubChunk       `json:"sub_chunks"`

	runtimeID uint64
}

// capturedUpdate is an UpdateBlock.
type capturedUpdate struct {
	Position [3]int32 `json:"position"`
	Layer    uint32   `json:"layer"`
	Hash     uint32   `json:"hash"`
}

// capturedSubChunk is a sub-chunk, by position, with the palettes of its
// layers: the network hashes of the states its blocks are of.
type capturedSubChunk struct {
	Position [3]int32  `json:"position"`
	Result   uint8     `json:"result"`
	Palettes [][]int32 `json:"palettes,omitempty"`
}

// readCommands reads the commands of a file, one a line; blank lines and
// lines starting with # are left out.
func readCommands(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(string(b), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	return out, nil
}

// runCommands runs the commands of the options as the player, each a
// moment after the one before, and returns what the server sent meanwhile
// and in the seconds after, with the sub-chunks around the player, which
// it asks for last.
func runCommands(conn *client.Conn, o options, commands []string) (*capture, error) {
	game := conn.StartGame()
	c := &capture{
		Protocol: o.protocol.Number, EditorLevelMigrationVersion: game.EditorLevelMigrationVersion,
		ClientBiomeUpdate: map[string]int{}, Commands: commands,
	}
	c.place(game.PlayerPosition)
	c.runtimeID = game.EntityRuntimeID
	// The server may move the player off the place StartGame gave it, as
	// BDS does from a height it holds until it found a spawn: the
	// commands and the sub-chunks asked for go where it is once moved.
	for start := time.Now(); time.Since(start) < 3*time.Second; {
		pk, err := conn.ReadPacket()
		if err != nil {
			var decodeErr *client.DecodeError
			if errors.As(err, &decodeErr) {
				continue
			}
			return nil, err
		}
		c.take(pk)
	}
	// The height of the player is not to be trusted: BDS holds a new
	// player at 32767 until it found it a spawn. The whole columns of
	// sub-chunks around it are asked for, from -64 to 319.
	sub := [3]int32{c.Origin[0] >> 4, 0, c.Origin[2] >> 4}
	go func() {
		for _, cmd := range commands {
			_ = conn.WritePacket(&mc.CommandRequestPacket{CommandLine: "/" + cmd, CommandOrigin: mc.CommandOrigin{PlayerUniqueID: game.EntityUniqueID}, Version: "latest"})
			_ = conn.Flush()
			time.Sleep(250 * time.Millisecond)
		}
		time.Sleep(2 * time.Second)
		var offsets []mc.SubChunkOffset
		for dx := int8(-1); dx <= 1; dx++ {
			for dy := int8(-4); dy < 20; dy++ {
				for dz := int8(-1); dz <= 1; dz++ {
					offsets = append(offsets, mc.SubChunkOffset{dx, dy, dz})
				}
			}
		}
		_ = conn.WritePacket(&mc.SubChunkRequestPacket{Dimension: game.Dimension, Position: sub, Offsets: offsets})
		_ = conn.Flush()
	}()
	deadline := time.Now().Add(time.Duration(len(commands))*250*time.Millisecond + 6*time.Second)
	for time.Now().Before(deadline) {
		pk, err := conn.ReadPacket()
		var decodeErr *client.DecodeError
		if errors.As(err, &decodeErr) {
			continue
		}
		if err != nil {
			return nil, err
		}
		c.take(pk)
	}
	return c, nil
}

// place sets the origin to the block at the feet of a player whose eyes
// are at a position.
func (c *capture) place(eyes [3]float32) {
	c.Origin = [3]int32{int32(math.Floor(float64(eyes[0]))), int32(math.Floor(float64(eyes[1]) - 1.62)), int32(math.Floor(float64(eyes[2])))}
}

// take records what the capture holds of a packet.
func (c *capture) take(pk any) {
	switch pk := pk.(type) {
	case *mc.MovePlayerPacket:
		if pk.EntityRuntimeID == c.runtimeID {
			c.place(pk.Position)
		}
	case *mc.CommandOutputPacket:
		for _, m := range pk.OutputMessages {
			c.Outputs = append(c.Outputs, fmt.Sprintf("%v %s %v", m.Success, m.Message, m.Parameters))
		}
	case *mc.LevelChunkPacket:
		c.ClientBiomeUpdate[strconv.FormatBool(pk.ClientBiomeUpdate)]++
	case *mc.DimensionDataPacket:
		c.Dimensions = append(c.Dimensions, pk.Definitions...)
	case *mc.UpdateBlockPacket:
		c.Updates = append(c.Updates, capturedUpdate{Position: pk.Position, Layer: pk.Layer, Hash: pk.NewBlockRuntimeID})
	case *mc.SubChunkPacket:
		for _, e := range pk.SubChunkEntries {
			s := capturedSubChunk{Result: e.Result, Position: [3]int32{
				pk.Position[0] + int32(e.Offset[0]), pk.Position[1] + int32(e.Offset[1]), pk.Position[2] + int32(e.Offset[2]),
			}}
			if raw, ok := e.RawPayload.Value(); ok {
				s.Palettes, _ = subChunkPalettes(raw)
			}
			c.SubChunks = append(c.SubChunks, s)
		}
	}
}

// subChunkPalettes returns the palettes of the layers of a sub-chunk of
// the network: the version (9), the count of layers and the Y, then each
// layer, a header of its bits a block and whether its values are runtime
// IDs, its words, the length of its palette and the palette, zigzag
// varints.
func subChunkPalettes(b []byte) ([][]int32, error) {
	if len(b) < 3 || b[0] != 9 {
		return nil, fmt.Errorf("a sub-chunk of version %v", b[:min(1, len(b))])
	}
	layers := int(b[1])
	b = b[3:]
	var out [][]int32
	for range layers {
		if len(b) == 0 {
			return out, errors.New("a sub-chunk cut short")
		}
		bits := int(b[0] >> 1)
		b = b[1:]
		n := 1
		if bits != 0 {
			perWord := 32 / bits
			words := (4096 + perWord - 1) / perWord
			if len(b) < words*4 {
				return out, errors.New("a sub-chunk cut short")
			}
			b = b[words*4:]
			v, read := varint32(b)
			if read <= 0 {
				return out, errors.New("a palette of no length")
			}
			n, b = int(v), b[read:]
		}
		palette := make([]int32, 0, n)
		for range n {
			v, read := varint32(b)
			if read <= 0 {
				return out, errors.New("a palette cut short")
			}
			palette, b = append(palette, v), b[read:]
		}
		out = append(out, palette)
	}
	return out, nil
}

// varint32 reads a zigzag varint.
func varint32(b []byte) (int32, int) {
	u, n := binary.Uvarint(b)
	if n <= 0 || u > math.MaxUint32 {
		return 0, -1
	}
	v := int32(uint32(u) >> 1)
	if u&1 != 0 {
		v = ^v
	}
	return v, n
}

// captureCommands runs the commands of -commands and writes what -capture
// holds.
func captureCommands(conn *client.Conn, o options) error {
	var commands []string
	if o.commands != "" {
		var err error
		if commands, err = readCommands(o.commands); err != nil {
			return err
		}
	}
	c, err := runCommands(conn, o, commands)
	if err != nil {
		return fmt.Errorf("capture: %w", err)
	}
	b, err := jsonFile(c)
	if err != nil {
		return err
	}
	return writeNewFileAtomic(o.capture, b)
}
