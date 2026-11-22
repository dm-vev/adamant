package nether

// ReleaseGenerationCaches drops the cached terrain, structure starts and
// feature records the generator keeps for the chunks around those it
// generated. What is dropped is computed again, in the same bytes, if
// generation resumes. It is safe to call while chunks are generated.
func (g *Nether) ReleaseGenerationCaches() {
	g.featureSource.terrain.clear()
	g.complexMu.Lock()
	clear(g.complexStarts)
	g.complexMu.Unlock()
	g.fossils.clear()
	g.portals.mu.Lock()
	clear(g.portals.starts)
	g.portals.mu.Unlock()
	g.features.ReleaseCaches()
}
