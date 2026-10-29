package world

import (
	"encoding/json"
	"math/rand/v2"
	"os"
	"testing"
)

func TestLevelRandomWordsMatchExecutedClient(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_level_random_seeded.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Seed  uint32
			Words []uint32
		}
		Bounded []struct {
			Seed, Bound uint32
			Values      []uint32
			NextWord    uint32 `json:"next_word"`
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 7 || len(native.Bounded) != 49 {
		t.Fatal("incomplete original RNG capture")
	}
	for _, c := range native.Scenarios {
		if len(c.Words) != 2000 {
			t.Fatal("missing original raw draws")
		}
		r := newLevelRandomSource(c.Seed)
		for i, want := range c.Words {
			if got := r.Uint32(); got != want {
				t.Fatalf("seed %d draw %d: %d, native %d", c.Seed, i, got, want)
			}
		}
	}
	for _, c := range native.Bounded {
		if len(c.Values) != 40 {
			t.Fatal("missing native bounded draws")
		}
		r := newLevelRandomSource(c.Seed)
		for i, want := range c.Values {
			if got := r.bounded(c.Bound); got != want {
				t.Fatalf("seed %d bound %d draw %d: %d, native %d", c.Seed, c.Bound, i, got, want)
			}
		}
		if got := r.Uint32(); got != c.NextWord {
			t.Fatalf("seed %d bound %d consumed wrong number of native draws", c.Seed, c.Bound)
		}
	}
}

func TestLevelRandomHeapStatesMatchExecutedClient(t *testing.T) {
	for _, file := range []string{"bedrock_1_26_51_level_random.json", "bedrock_1_26_51_level_random_high_seed.json"} {
		data, err := os.ReadFile("../block/testdata/" + file)
		if err != nil {
			t.Fatal(err)
		}
		var native struct {
			Complete    bool
			LevelSeed   uint64 `json:"level_seed"`
			Owner       struct{ Seed uint32 }
			Checkpoints []struct {
				Seed        uint32
				State       [624]uint32
				Index       int
				NextWords   []uint32 `json:"next_words"`
				DrawsToNext int      `json:"draws_to_next"`
			}
		}
		if err := json.Unmarshal(data, &native); err != nil {
			t.Fatal(err)
		}
		if !native.Complete || len(native.Checkpoints) != 20 || native.Owner.Seed != uint32(native.LevelSeed) {
			t.Fatal("incomplete native RNG/seed controls")
		}
		for i, c := range native.Checkpoints {
			if c.Index < 0 || c.Index > 624 || c.Seed != native.Owner.Seed || len(c.NextWords) != 16 {
				t.Fatal("invalid native RNG checkpoint")
			}
			r := &levelRandomSource{state: c.State, index: c.Index}
			for n, want := range c.NextWords {
				if got := r.Uint32(); got != want {
					t.Fatalf("%s checkpoint %d draw %d: %d, native %d", file, i, n, got, want)
				}
			}
			if i+1 < len(native.Checkpoints) {
				r = &levelRandomSource{state: c.State, index: c.Index}
				for range c.DrawsToNext {
					r.Uint32()
				}
				next := native.Checkpoints[i+1]
				if r.index != next.Index || r.state != next.State {
					t.Fatalf("%s checkpoint %d native state transition differs after %d draws", file, i, c.DrawsToNext)
				}
			}
		}
	}
}

func TestLevelRandomRawDrawsShareTheWorldStream(t *testing.T) {
	settings := (NopProvider{}).Settings()
	settings.Seed = 4294967298
	settings.SeedSet = true
	w := Config{Synchronous: true, Provider: NopProvider{Set: settings}, RandomTickSpeed: -1}.New()
	defer w.Close()
	<-w.Exec(func(tx *Tx) {
		source, ok := w.conf.RandSource.(*levelRandomSource)
		if !ok {
			t.Fatal("default source is not native")
		}
		control := *source // World.New may already have consumed initial weather draws.
		if got, want := tx.RandomUint32(), control.Uint32(); got != want {
			t.Fatal("raw draw discarded an additional native word")
		}
		want64 := control.Uint64()
		if got := w.r.Uint64(); got != want64 {
			t.Fatal("Go adapter does not share the native stream")
		}
		if got, want := (worldRandomWords{w}).Uint32(), control.Uint32(); got != want {
			t.Fatal("actor shuffle does not share the native stream")
		}
	})
	// A caller-supplied source keeps its existing Go semantics.
	custom := rand.NewPCG(3, 4)
	other := Config{Synchronous: true, RandSource: custom, RandomTickSpeed: -1}.New()
	defer other.Close()
	if other.conf.RandSource != custom {
		t.Fatal("explicit source was replaced")
	}
}
