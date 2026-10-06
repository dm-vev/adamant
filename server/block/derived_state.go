package block

// Derived-state property lists are shared read-only metadata. Generated chunks
// reconstruct these fields once their neighbouring chunks are available.
var cardinalConnectionProperties = []string{
	"minecraft:connection_north", "minecraft:connection_east",
	"minecraft:connection_south", "minecraft:connection_west",
}
var stairCornerProperties = []string{"minecraft:corner"}

func (GlassPane) DerivedStateProperties() []string         { return cardinalConnectionProperties }
func (StainedGlassPane) DerivedStateProperties() []string  { return cardinalConnectionProperties }
func (HardenedGlassPane) DerivedStateProperties() []string { return cardinalConnectionProperties }
func (HardenedStainedGlassPane) DerivedStateProperties() []string {
	return cardinalConnectionProperties
}
func (IronBars) DerivedStateProperties() []string         { return cardinalConnectionProperties }
func (CopperBars) DerivedStateProperties() []string       { return cardinalConnectionProperties }
func (WoodFence) DerivedStateProperties() []string        { return cardinalConnectionProperties }
func (NetherBrickFence) DerivedStateProperties() []string { return cardinalConnectionProperties }
func (Tripwire) DerivedStateProperties() []string         { return cardinalConnectionProperties }
func (Stairs) DerivedStateProperties() []string           { return stairCornerProperties }

func (Stairs) ReconstructionOrder() int                   { return 0 }
func (Tripwire) ReconstructionOrder() int                 { return 0 }
func (GlassPane) ReconstructionOrder() int                { return 1 }
func (StainedGlassPane) ReconstructionOrder() int         { return 1 }
func (HardenedGlassPane) ReconstructionOrder() int        { return 1 }
func (HardenedStainedGlassPane) ReconstructionOrder() int { return 1 }
func (IronBars) ReconstructionOrder() int                 { return 1 }
func (CopperBars) ReconstructionOrder() int               { return 1 }
func (WoodFence) ReconstructionOrder() int                { return 1 }
func (NetherBrickFence) ReconstructionOrder() int         { return 1 }
