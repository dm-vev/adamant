package overworld

import (
	"container/list"

	"github.com/df-mc/dragonfly/server/world"
)

// ReleaseGenerationCaches drops what the generator keeps for the chunks
// around those it generated: the terrain previews and the records of what
// the features of each origin write, which decoration reads up to three
// chunks away. Each is bounded, yet a bound sized for the front of a player
// at a view distance of 32 chunks holds hundreds of megabytes of a front that
// stopped moving. What is dropped is computed again, in the same bytes, if
// generation resumes. It is safe to call while chunks are generated.
func (g *Overworld) ReleaseGenerationCaches() {
	g.previewCache.clear()
	g.rawCache.clear()
	g.chunkBiomes.clear()
	g.worldgenHeights.clear()
	g.modernFeatures.ReleaseCaches()
}

// ReleaseCaches drops the records of the origins computed, which the chunks
// around an origin still to be decorated would share; one being computed is
// kept for those waiting for it.
func (f *ModernFeatures) ReleaseCaches() {
	f.origins.clear()
	f.contexts.clear()
}

// clear drops the previews of every shard.
func (c *previewCache) clear() {
	for i := range c.shards {
		s := &c.shards[i]
		s.mu.Lock()
		s.order = list.New()
		s.index = make(map[world.ChunkPos]*list.Element, s.cap)
		s.mu.Unlock()
	}
}

// clear drops the noise biomes of every shard.
func (c *chunkBiomeCache) clear() {
	for i := range c.shards {
		s := &c.shards[i]
		s.mu.Lock()
		s.biomes = nil
		s.mu.Unlock()
	}
}

// clear drops the entries whose record is computed. Whoever holds an entry
// keeps it; an entry being computed stays for those waiting for it.
func (c *originRecordCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for el := c.recent.Front(); el != nil; {
		next := el.Next()
		entry := el.Value.(*originRecordEntry)
		select {
		case <-entry.done:
			c.remove(entry)
		default:
		}
		el = next
	}
	if len(c.entries) == 0 {
		c.entries = nil
	}
}
