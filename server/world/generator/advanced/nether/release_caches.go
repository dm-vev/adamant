package nether

// ReleaseGenerationCaches drops the records of the features the generator
// keeps for the chunks around those it generated. What is dropped is
// computed again, in the same bytes, if generation resumes. It is safe to
// call while chunks are generated.
func (g *Nether) ReleaseGenerationCaches() {
	g.features.ReleaseCaches()
}
