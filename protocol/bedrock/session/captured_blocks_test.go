package session

import (
	"encoding/json"
	"flag"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
)

var updateCapturedBlocks = flag.Bool("update-captured-blocks", false, "rewrite the block states of the network block hashes BDS was captured sending")

// capturedBlocksPath holds the block state of each network block hash that
// BDS was captured sending, in the captures of the block package, with event
// sounds, block break particles and block updates. The game's tests of those
// captures read it to expect the block of a sound or particle, as the game
// does not hash blocks for Bedrock clients.
const capturedBlocksPath = "../../../server/block/testdata/bedrock_1_26_51_captured_blocks.json"

// soundsWithBlocks are the events whose ExtraData BDS fills with the network
// block hash of a block.
var soundsWithBlocks = map[string]bool{
	"button.click_on": true, "button.click_off": true,
	"pressure_plate.click_on": true, "pressure_plate.click_off": true,
	"attach": true, "detach": true, "power.on": true, "power.off": true,
	"crafter.craft": true, "crafter.fail": true,
	"block.click": true, "block.click.fail": true,
}

// hookSoundIDs are the IDs of the events of the hook capture, which records
// the IDs rather than the names.
var hookSoundIDs = map[int]string{73: "power.on", 74: "power.off", 75: "attach", 76: "detach"}

// levelEventParticlesDestroyBlock is the LevelEvent of block break
// particles, whose EventData is the network block hash of the block.
const levelEventParticlesDestroyBlock = 2001

// capturedSound is an event sound as the captures record it.
type capturedSound struct {
	SoundType string
	ExtraData int32
}

// blockCaptures are the captures of BDS in the block package that hold
// network block hashes, and how to read the hashes from each.
var blockCaptures = []struct {
	name   string
	hashes func(data []byte) ([]int32, error)
}{
	{"bedrock_1_26_51_switch_sounds.json", topSoundHashes},
	{"bedrock_1_26_51_plate_context.json", topSoundHashes},
	{"bedrock_1_26_51_tripwire_break.json", topSoundHashes},
	{"bedrock_1_26_51_hook_placement.json", topSoundHashes},
	{"bedrock_1_26_51_crafter_sounds.json", func(data []byte) ([]int32, error) {
		var capture struct {
			Scenarios []struct{ Sounds []capturedSound }
		}
		err := json.Unmarshal(data, &capture)
		var hashes []int32
		for _, sc := range capture.Scenarios {
			hashes = append(hashes, soundHashes(sc.Sounds)...)
		}
		return hashes, err
	}},
	{"bedrock_1_26_51_hook_events.json", func(data []byte) ([]int32, error) {
		var capture struct {
			Scenarios []struct {
				Output []struct {
					Kind      string
					SoundID   int   `json:"sound_id"`
					ExtraData int32 `json:"extra_data"`
				}
			}
		}
		err := json.Unmarshal(data, &capture)
		var hashes []int32
		for _, sc := range capture.Scenarios {
			for _, o := range sc.Output {
				if o.Kind == "sound" && soundsWithBlocks[hookSoundIDs[o.SoundID]] {
					hashes = append(hashes, o.ExtraData)
				}
			}
		}
		return hashes, err
	}},
	{"bedrock_1_26_51_tnt_states.json", func(data []byte) ([]int32, error) {
		var capture struct {
			Scenarios []struct {
				Accepted    bool
				NetworkHash uint32 `json:"network_hash"`
			}
		}
		err := json.Unmarshal(data, &capture)
		var hashes []int32
		for _, sc := range capture.Scenarios {
			if sc.Accepted {
				hashes = append(hashes, int32(sc.NetworkHash))
			}
		}
		return hashes, err
	}},
	{"bedrock_1_26_51_piston_player_break_creative.json", breakParticleHashes},
	{"bedrock_1_26_51_piston_player_break_survival.json", breakParticleHashes},
}

// topSoundHashes returns the network block hashes of the event sounds of a
// capture that lists its sounds at its top.
func topSoundHashes(data []byte) ([]int32, error) {
	var capture struct{ Sounds []capturedSound }
	err := json.Unmarshal(data, &capture)
	return soundHashes(capture.Sounds), err
}

// soundHashes returns the network block hashes of the event sounds passed
// that carry one.
func soundHashes(sounds []capturedSound) []int32 {
	var hashes []int32
	for _, s := range sounds {
		if soundsWithBlocks[s.SoundType] {
			hashes = append(hashes, s.ExtraData)
		}
	}
	return hashes
}

// breakParticleHashes returns the network block hashes of the block break
// particles of a capture of pistons broken.
func breakParticleHashes(data []byte) ([]int32, error) {
	var capture struct {
		Scenarios []struct {
			LevelEvents []struct {
				EventType int32
				EventData int32
			} `json:"level_events"`
		}
	}
	err := json.Unmarshal(data, &capture)
	var hashes []int32
	for _, sc := range capture.Scenarios {
		for _, e := range sc.LevelEvents {
			if e.EventType == levelEventParticlesDestroyBlock {
				hashes = append(hashes, e.EventData)
			}
		}
	}
	return hashes, err
}

// capturedBlockHashes returns every network block hash of blockCaptures.
func capturedBlockHashes(t *testing.T) map[int32]bool {
	t.Helper()
	hashes := map[int32]bool{}
	for _, c := range blockCaptures {
		data, err := os.ReadFile("../../../server/block/testdata/" + c.name)
		if err != nil {
			t.Fatal(err)
		}
		found, err := c.hashes(data)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if len(found) == 0 {
			t.Fatalf("%s holds no network block hash", c.name)
		}
		for _, hash := range found {
			hashes[hash] = true
		}
	}
	return hashes
}

// capturedBlockState names a block state as capturedBlocksPath does: its
// name and its properties, sorted, as the block encodes them.
func capturedBlockState(b world.Block) string {
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

// TestCapturedBlockHashes checks the block states capturedBlocksPath holds
// for the network block hashes BDS was captured sending: the hash of each is
// the one it is listed by, and every hash of the captures is listed. The
// game's tests expect the states; this test, that they reach Bedrock clients
// as BDS sent them. Run with -update-captured-blocks to write the file again
// from the captures.
func TestCapturedBlockHashes(t *testing.T) {
	br := world.DefaultBlockRegistry
	br.Finalize()
	hashes := capturedBlockHashes(t)
	if *updateCapturedBlocks {
		states := map[string]string{}
		for hash := range hashes {
			rid, ok := hashesOf(br).hashToRuntimeID(uint32(hash))
			if !ok {
				t.Fatalf("no block state of network hash %d", hash)
			}
			states[strconv.Itoa(int(hash))] = capturedBlockState(br.BlockByRuntimeIDOrAir(rid))
		}
		data, err := json.MarshalIndent(states, "", "\t")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(capturedBlocksPath, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	data, err := os.ReadFile(capturedBlocksPath)
	if err != nil {
		t.Fatal(err)
	}
	var states map[string]string
	if err := json.Unmarshal(data, &states); err != nil {
		t.Fatal(err)
	}
	blocks := map[string]world.Block{}
	for _, b := range br.Blocks() {
		blocks[capturedBlockState(b)] = b
	}
	for key, state := range states {
		hash, err := strconv.ParseInt(key, 10, 32)
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		if !hashes[int32(hash)] {
			t.Errorf("%s lists %d, which no capture holds", capturedBlocksPath, hash)
		}
		b, ok := blocks[state]
		if !ok {
			t.Errorf("no block of the state %s", state)
			continue
		}
		if got := int32(networkBlockID(br, b)); got != int32(hash) {
			t.Errorf("%s hashes to %d, BDS sent %d", state, got, hash)
		}
	}
	for hash := range hashes {
		if _, ok := states[strconv.Itoa(int(hash))]; !ok {
			t.Errorf("%s lacks the network hash %d", capturedBlocksPath, hash)
		}
	}
}
