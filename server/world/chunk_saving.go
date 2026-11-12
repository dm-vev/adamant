package world

import (
	"fmt"
	"strings"
)

// ChunkSaving selects which loaded columns a World stores when it saves and
// when it unloads them.
type ChunkSaving int

const (
	// ChunkSavingModified stores a column only if the provider may lack what
	// it holds: a column generated or changed since its last save, whole,
	// and one whose entities or block entities may have changed since,
	// because they ticked or were used, without its blocks if the provider
	// can (ActorStorer). A column read from the provider and left unchanged
	// is not written back.
	ChunkSavingModified ChunkSaving = iota
	// ChunkSavingAll stores every column loaded, read from the provider or
	// generated, at every save and when it is unloaded, changed or not. It
	// suits pregenerating a world, and costs a write of every loaded column
	// at each save.
	ChunkSavingAll
)

// ParseChunkSaving parses "modified" or "all", in any case and with spaces
// around. An empty string is "modified".
func ParseChunkSaving(s string) (ChunkSaving, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "modified":
		return ChunkSavingModified, nil
	case "all":
		return ChunkSavingAll, nil
	}
	return ChunkSavingModified, fmt.Errorf("unknown SaveChunks %q: use \"modified\" or \"all\"", s)
}

// String returns the name ParseChunkSaving parses.
func (s ChunkSaving) String() string {
	if s == ChunkSavingAll {
		return "all"
	}
	return "modified"
}

// storesColumn reports whether saving or unloading c, the column at pos,
// stores it. With ChunkSavingAll a column holding what the provider or the
// generator gave it is stored even unchanged; a column standing in for one
// that failed to load is never stored, nor is the air left in place of a
// generation cut short.
func (w *World) storesColumn(pos ChunkPos, c *Column) bool {
	if !c.needsSave() && (w.conf.SaveChunks != ChunkSavingAll || !c.complete) {
		return false
	}
	return !w.failedLoad(pos, c)
}
