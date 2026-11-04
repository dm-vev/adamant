package overworld

import "github.com/df-mc/dragonfly/server/world/generator/advanced/internal/structure"

// Only tests use these helpers of modern_feature_template_fluids.go, so they live with the tests.

// applyTemplateFluids matches StructureTemplate's post-write waterlogging pass.
// Native secondary layers retain the actual fluid kind and source/flowing state.
func applyTemplateFluids(region *featureRegion, blocks []structure.Block) []structure.Block {
	return applyTemplateFluidsForStructure(region, blocks, false)
}

func applyTemplateFluidsForStructure(region *featureRegion, blocks []structure.Block, structurePieceWrites bool) []structure.Block {
	payload, _ := applyTemplateFluidsForStructureWrites(region, blocks, structurePieceWrites, 3)
	return payload
}
