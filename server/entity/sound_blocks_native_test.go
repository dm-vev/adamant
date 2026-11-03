package entity

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

// nativeSoundBlocks returns the block states of
// ../block/testdata/bedrock_1_26_51_sound_blocks.json by the network block hash BDS
// sent each as with an event sound in the captures of the block package. The
// Bedrock session checks the hashes; the game's tests expect the states.
func nativeSoundBlocks(t testing.TB) map[int32]string {
	t.Helper()
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_sound_blocks.json")
	if err != nil {
		t.Fatal(err)
	}
	var states map[string]string
	if err := json.Unmarshal(data, &states); err != nil {
		t.Fatal(err)
	}
	blocks := make(map[int32]string, len(states))
	for key, state := range states {
		hash, err := strconv.ParseInt(key, 10, 32)
		if err != nil {
			t.Fatal(err)
		}
		blocks[int32(hash)] = state
	}
	return blocks
}

// nativeSoundBlockState names the state of a block as nativeSoundBlocks
// does: its name and its properties, sorted, as the block encodes them.
func nativeSoundBlockState(b world.Block) string {
	if b == nil {
		return "<nil>"
	}
	name, props := b.EncodeBlock()
	var s strings.Builder
	s.WriteString(name)
	for i, k := range slices.Sorted(maps.Keys(props)) {
		sep := " "
		if i == 0 {
			sep = "["
		}
		fmt.Fprintf(&s, "%s%s=%v", sep, k, props[k])
	}
	if len(props) != 0 {
		s.WriteString("]")
	}
	return s.String()
}
