package block

// LeashAttachable reports whether leads may be tied to this block.
type LeashAttachable interface {
	LeashAttachable() bool
}

func (WoodFence) LeashAttachable() bool        { return true }
func (NetherBrickFence) LeashAttachable() bool { return true }
