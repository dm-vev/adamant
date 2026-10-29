package world

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
)

type nativeActorDraws struct {
	draws []uint32
	used  int
}

func (s *nativeActorDraws) Uint64() uint64 {
	if s.used == len(s.draws) {
		panic("actor shuffle consumed an extra random draw")
	}
	word := s.draws[s.used]
	s.used++
	return uint64(word) << 32
}

func TestBlockActorShuffleMatchesExecutedClient(t *testing.T) {
	data, err := os.ReadFile("../block/testdata/bedrock_1_26_51_actor_order.json")
	if err != nil {
		t.Fatal(err)
	}
	var native struct {
		Complete  bool
		Scenarios []struct {
			Count int
			Draws []uint32
			Order []int
		}
	}
	if err := json.Unmarshal(data, &native); err != nil {
		t.Fatal(err)
	}
	if !native.Complete || len(native.Scenarios) != 130 {
		t.Fatal("incomplete executed-client actor shuffle capture")
	}
	permutations := map[string]bool{}
	for i, c := range native.Scenarios {
		t.Run(fmt.Sprintf("%d/n%d", i, c.Count), func(t *testing.T) {
			positions := make([]int, c.Count)
			for i := range positions {
				positions[i] = i
			}
			source := &nativeActorDraws{draws: c.Draws}
			got := appendShuffledBlockEntities([]int{-2, -1}, positions, rand.New(source))
			if !slices.Equal(got[:2], []int{-2, -1}) || !slices.Equal(got[2:], c.Order) {
				t.Fatalf("shuffle %v, executed client %v", got, c.Order)
			}
			if source.used != len(c.Draws) {
				t.Fatalf("consumed %d native draws, want %d", source.used, len(c.Draws))
			}
			for i, p := range positions {
				if p != i {
					t.Fatal("shuffle mutated the stored actor order")
				}
			}
			if c.Count == 4 {
				permutations[fmt.Sprint(c.Order)] = true
			}
		})
	}
	if len(permutations) != 24 {
		t.Fatalf("native capture exercised %d/24 four-actor orders", len(permutations))
	}
}

type actorIdentityTicker struct {
	testTickerBlock
	callback func(*Tx)
}

func (b *actorIdentityTicker) Tick(_ int64, _ cube.Pos, tx *Tx) {
	b.ticks++
	if b.callback != nil {
		b.callback(tx)
	}
}

// Keep the two-entry native shuffle in insertion order. Other world randomness
// is irrelevant to these viewerless, nonrandom-block-tick scenarios.
type actorIdentityRandom struct{}

func (actorIdentityRandom) Uint64() uint64 { return 0x8000000100000000 }

type chunkActorOrderSource struct{ index int }

func (r *chunkActorOrderSource) Uint64() uint64 {
	words := [...]uint32{0x80000000, 0x80000001, 0x80000001}
	i := r.index
	r.index++
	if i >= len(words) {
		return 0x8000000100000008
	}
	return uint64(words[i])<<32 | 8
}

func TestChunkActorCallbacksPrecedeTheNextChunkShuffle(t *testing.T) {
	// LevelChunk::tickImpl calls tickBlockEntities at 0x32cab8f before
	// returning to its caller. The next chunk's shuffle must therefore see
	// RNG draws consumed by this chunk's actors. randomTickSpeed zero also
	// disables precipitation, leaving two actor words and one shuffle word.
	source := &chunkActorOrderSource{}
	settings := (NopProvider{}).Settings()
	settings.WeatherCycle = false
	w := Config{Synchronous: true, RandomTickSpeed: -1, RandSource: source, Provider: NopProvider{Set: settings}}.New()
	defer w.Close()
	var order []string
	var draws []uint32
	a, b, c := &actorIdentityTicker{}, &actorIdentityTicker{}, &actorIdentityTicker{}
	a.callback = func(tx *Tx) { order = append(order, "a"); draws = append(draws, tx.RandomUint32(), tx.RandomUint32()) }
	b.callback = func(*Tx) { order = append(order, "b") }
	c.callback = func(*Tx) { order = append(order, "c") }
	<-w.Exec(func(tx *Tx) {
		tx.chunk(ChunkPos{0, 0}).updateBlockEntity(cube.Pos{0, 4, 0}, a)
		later := tx.chunk(ChunkPos{1, 0})
		later.updateBlockEntity(cube.Pos{16, 4, 0}, b)
		later.updateBlockEntity(cube.Pos{17, 4, 0}, c)
		source.index = 0
	})
	w.AdvanceTick()
	if fmt.Sprint(order) != "[a b c]" || fmt.Sprint(draws) != "[2147483648 2147483649]" || source.index != 3 {
		t.Fatalf("chunk actor sequence=%v raw draws=%v words consumed=%d", order, draws, source.index)
	}
}

func TestBlockActorReplacementDuringTickWaitsUntilNextTick(t *testing.T) {
	for _, change := range []string{"remove", "replace", "update-data"} {
		t.Run(change, func(t *testing.T) {
			w := Config{Synchronous: true, RandomTickSpeed: -1, RandSource: actorIdentityRandom{}}.New()
			defer w.Close()
			first, old, replacement := &actorIdentityTicker{}, &actorIdentityTicker{}, &actorIdentityTicker{}
			var col *Column
			first.callback = func(*Tx) {
				first.callback = nil
				if change == "update-data" {
					col.updateBlockEntity(cube.Pos{1, 4, 0}, old)
					return
				}
				col.updateBlockEntity(cube.Pos{1, 4, 0}, nil)
				if change == "replace" {
					col.updateBlockEntity(cube.Pos{1, 4, 0}, replacement)
				}
			}
			<-w.Exec(func(tx *Tx) {
				col = tx.chunk(ChunkPos{})
				col.updateBlockEntity(cube.Pos{0, 4, 0}, first)
				col.updateBlockEntity(cube.Pos{1, 4, 0}, old)
			})
			w.AdvanceTick()
			wantOld := 0
			if change == "update-data" {
				wantOld = 1
			}
			if old.ticks != wantOld || replacement.ticks != 0 {
				t.Fatalf("first tick: old=%d replacement=%d", old.ticks, replacement.ticks)
			}
			w.AdvanceTick()
			wantReplacement := 0
			if change == "replace" {
				wantReplacement = 1
			}
			if replacement.ticks != wantReplacement {
				t.Fatalf("next tick: replacement=%d, want %d", replacement.ticks, wantReplacement)
			}
		})
	}
}

func TestNewBlockActorEligibilityFollowsChunkVisitOrder(t *testing.T) {
	// Native storage appends a newly placed actor immediately (client
	// 0x32c11ac..0x32c11f1), while tickBlockEntities copies only the chunk
	// currently being visited. A destination chunk still ahead in this pass
	// can therefore tick a new actor; a chunk already visited must wait.
	for _, later := range []bool{false, true} {
		t.Run(fmt.Sprint("destination_later_", later), func(t *testing.T) {
			w := Config{Synchronous: true, RandomTickSpeed: -1, RandSource: actorIdentityRandom{}}.New()
			defer w.Close()
			first, born := &actorIdentityTicker{}, &actorIdentityTicker{}
			origin, destination := ChunkPos{1, 0}, ChunkPos{0, 0}
			if later {
				origin, destination = destination, origin
			}
			<-w.Exec(func(tx *Tx) {
				target := tx.chunk(destination)
				first.callback = func(*Tx) {
					first.callback = nil
					target.updateBlockEntity(cube.Pos{int(destination[0]) * 16, 4, 0}, born)
				}
				tx.chunk(origin).updateBlockEntity(cube.Pos{int(origin[0]) * 16, 4, 0}, first)
			})
			w.AdvanceTick()
			want := 0
			if later {
				want = 1
			}
			if born.ticks != want {
				t.Fatalf("creation tick: actor ticks=%d, want %d", born.ticks, want)
			}
			w.AdvanceTick()
			if born.ticks != want+1 {
				t.Fatalf("following tick: actor ticks=%d, want %d", born.ticks, want+1)
			}
		})
	}
}
