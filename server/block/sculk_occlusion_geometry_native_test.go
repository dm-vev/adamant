package block

import "testing"

func TestSculkOcclusionGeometryMatchesBDS(t *testing.T) {
	testSculkNativeContacts(t, "bedrock_1_26_51_sculk_occlusion_geometry.json", 168)
}
