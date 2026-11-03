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

var updateSoundBlocks = flag.Bool("update-sound-blocks", false, "rewrite the block states of the sounds BDS was captured sending")

// soundBlocksPath holds the block state of each network block hash that BDS
// was captured sending with an event sound, in the captures of the block
// package. The game's tests of those captures read it to expect the block of
// a sound.Event, as the game cannot hash a block for Bedrock clients.
const soundBlocksPath = "../../../server/block/testdata/bedrock_1_26_51_sound_blocks.json"

// soundBlockCaptures are the captures of BDS in the block package whose event
// sounds carry a network block hash.
var soundBlockCaptures = []string{
	"bedrock_1_26_51_switch_sounds.json",
	"bedrock_1_26_51_plate_context.json",
	"bedrock_1_26_51_hook_events.json",
	"bedrock_1_26_51_crafter_sounds.json",
	"bedrock_1_26_51_tripwire_break.json",
	"bedrock_1_26_51_hook_placement.json",
}

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

// capturedSoundBlockHashes returns every network block hash the captures of
// soundBlockCaptures hold in an event sound.
func capturedSoundBlockHashes(t *testing.T) map[int32]bool {
	t.Helper()
	type capturedSound struct {
		SoundType string
		ExtraData int32
	}
	var capture struct {
		Sounds    []capturedSound
		Scenarios []struct{ Sounds []capturedSound }
	}
	// The hook capture records what each scenario outputs, sounds by ID.
	var hooks struct {
		Scenarios []struct {
			Output []struct {
				Kind      string
				SoundID   int   `json:"sound_id"`
				ExtraData int32 `json:"extra_data"`
			}
		}
	}
	hashes := map[int32]bool{}
	found := 0
	add := func(s capturedSound) {
		if soundsWithBlocks[s.SoundType] {
			hashes[s.ExtraData] = true
			found++
		}
	}
	for _, name := range soundBlockCaptures {
		data, err := os.ReadFile("../../../server/block/testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		capture.Sounds, capture.Scenarios = nil, nil
		found = 0
		if name == "bedrock_1_26_51_hook_events.json" {
			if err := json.Unmarshal(data, &hooks); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			for _, sc := range hooks.Scenarios {
				for _, o := range sc.Output {
					if o.Kind == "sound" {
						add(capturedSound{hookSoundIDs[o.SoundID], o.ExtraData})
					}
				}
			}
		} else if err := json.Unmarshal(data, &capture); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, s := range capture.Sounds {
			add(s)
		}
		for _, sc := range capture.Scenarios {
			for _, s := range sc.Sounds {
				add(s)
			}
		}
		if found == 0 {
			t.Fatalf("%s holds no sound of a block", name)
		}
	}
	return hashes
}

// soundBlockState names a block state as soundBlocksPath does: its name and
// its properties, sorted, as the block encodes them.
func soundBlockState(b world.Block) string {
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

// TestSoundBlocksHashAsCaptured checks the block states soundBlocksPath holds
// for the network block hashes BDS sent with event sounds: the hash of each
// is the one it is listed by, and every hash of the captures is listed. The
// game's tests expect the states; this test, that they reach Bedrock clients
// as BDS sent them. Run with -update-sound-blocks to write the file again
// from the captures.
func TestSoundBlocksHashAsCaptured(t *testing.T) {
	br := world.DefaultBlockRegistry
	br.Finalize()
	hashes := capturedSoundBlockHashes(t)
	if *updateSoundBlocks {
		states := map[string]string{}
		for hash := range hashes {
			rid, ok := br.HashToRuntimeID(uint32(hash))
			if !ok {
				t.Fatalf("no block state of network hash %d", hash)
			}
			states[strconv.Itoa(int(hash))] = soundBlockState(br.BlockByRuntimeIDOrAir(rid))
		}
		data, err := json.MarshalIndent(states, "", "\t")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(soundBlocksPath, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	data, err := os.ReadFile(soundBlocksPath)
	if err != nil {
		t.Fatal(err)
	}
	var states map[string]string
	if err := json.Unmarshal(data, &states); err != nil {
		t.Fatal(err)
	}
	blocks := map[string]world.Block{}
	for _, b := range br.Blocks() {
		blocks[soundBlockState(b)] = b
	}
	for key, state := range states {
		hash, err := strconv.ParseInt(key, 10, 32)
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		if !hashes[int32(hash)] {
			t.Errorf("%s lists %d, which no capture holds", soundBlocksPath, hash)
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
			t.Errorf("%s lacks the network hash %d", soundBlocksPath, hash)
		}
	}
}
