package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

type nativeContainerCheckpoint struct {
	Tick    int64
	Outside []struct {
		Value             uint32
		Bound             uint32
		BoundedCaller     string    `json:"bounded_caller"`
		ContainerPosition *cube.Pos `json:"container_position"`
	}
	Chunks []struct {
		Position world.ChunkPos
		Words    []struct {
			Value  uint32
			Caller string
		}
	}
}

// nativeContainerRandom feeds independently observed actor-pass words through
// Go's existing rand.Source contract: Uint32 consumers read its high bits.
// randomTickSpeed is zero in both captures and replays, so no precipitation
// draws intervene. No production test hook or random-source API is needed.
type nativeContainerRandom struct {
	words  []uint64
	used   int
	active bool
}

func (r *nativeContainerRandom) Uint64() uint64 {
	if !r.active {
		return 8
	}
	if r.used == len(r.words) {
		panic("container replay consumed an unobserved random draw")
	}
	x := r.words[r.used]
	r.used++
	return x
}

func (r *nativeContainerRandom) prepare(t *testing.T, tx *world.Tx, tick int64, source, target cube.Pos, checkpoints []nativeContainerCheckpoint) {
	t.Helper()
	if len(checkpoints) != 256 {
		t.Fatal("incomplete native actor-input capture")
	}
	chunkAt := func(p cube.Pos) world.ChunkPos { return world.ChunkPos{int32(p[0] >> 4), int32(p[2] >> 4)} }
	base, destination := chunkAt(source), chunkAt(target)
	byChunk := map[world.ChunkPos][]uint32{}
	var scheduledWords []uint32
	found := false
	for _, checkpoint := range checkpoints {
		if checkpoint.Tick != tick {
			continue
		}
		if found {
			t.Fatal("duplicate native actor-input checkpoint")
		}
		found = true
		for _, word := range checkpoint.Outside {
			if word.ContainerPosition == nil || *word.ContainerPosition != source {
				continue
			}
			switch word.BoundedCaller {
			case "0x8a2ca55", "0x57f0d77":
			default:
				t.Fatalf("unexpected native scheduled container RNG caller %s", word.BoundedCaller)
			}
			if word.Bound < 1 || word.Bound > 9 {
				t.Fatal("invalid native container RNG bound")
			}
			scheduledWords = append(scheduledWords, word.Value)
		}
		for _, chunk := range checkpoint.Chunks {
			if chunk.Position != base && chunk.Position != destination {
				continue
			}
			words := []uint32{}
			for _, word := range chunk.Words {
				switch word.Caller {
				case "0x9fd6b3", "0x57f82d6", "0x57f82f5":
				default:
					t.Fatalf("unexpected native actor RNG caller %s", word.Caller)
				}
				words = append(words, word.Value)
			}
			// The first checkpoint also contains startup passes. The last
			// pass of each chunk is the sampled tick, held in Script API.
			byChunk[chunk.Position] = words
		}
	}
	if !found {
		t.Fatal("native actor-input checkpoint missing")
	}
	if _, ok := byChunk[base]; !ok {
		t.Fatal("native source chunk pass missing")
	}
	if _, ok := byChunk[destination]; !ok {
		t.Fatal("native destination chunk pass missing")
	}
	r.words, r.used, r.active = r.words[:0], 0, false
	for _, word := range scheduledWords {
		r.words = append(r.words, uint64(word)<<32|8)
	}
	// This isolated fixture loads a radius-two world. Synchronous worlds
	// process loaded columns in x/z order; probe a larger enclosing area to
	// include any neighbouring columns loaded by block placement.
	for x := base[0] - 4; x <= base[0]+4; x++ {
		for z := base[1] - 4; z <= base[1]+4; z++ {
			pos := world.ChunkPos{x, z}
			if !tx.ChunkLoaded(pos) {
				continue
			}
			for _, word := range byChunk[pos] {
				r.words = append(r.words, uint64(word)<<32|8)
			}
		}
	}
	r.active = true
}
