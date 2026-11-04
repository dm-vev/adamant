package block

// MaxCommandBlockText bounds each text of a CommandBlockEdit, in bytes: the
// longest string the game reads from NBT.
const MaxCommandBlockText = 32767

// CommandBlockEdit is what a player sets in the editor of a command block or
// of a command block minecart.
type CommandBlockEdit struct {
	// Mode is when the command block runs. A minecart has no mode.
	Mode CommandBlockMode
	// Conditional and NeedsRedstone are the conditions of a command block to
	// run. A minecart has neither.
	Conditional, NeedsRedstone bool
	// Command is the command run, Name the name of the command block and
	// LastOutput the output it last had, which only a minecart takes.
	Command, Name, LastOutput string
	// TrackOutput is whether the command block keeps the output of its
	// command.
	TrackOutput bool
	// TickDelay is the number of ticks the command block waits before it
	// runs.
	TickDelay int
	// ExecuteOnFirstTick is whether a repeating command block runs on the
	// first tick it is active.
	ExecuteOnFirstTick bool
}

// ValidText reports whether every text of the edit is at most
// MaxCommandBlockText bytes long.
func (e CommandBlockEdit) ValidText() bool {
	return len(e.Command) <= MaxCommandBlockText && len(e.Name) <= MaxCommandBlockText && len(e.LastOutput) <= MaxCommandBlockText
}

// Edited returns b with the settings of e applied. Its last output is cleared
// unless it tracks its output, and is otherwise kept: the output of an edit is
// that of a minecart.
func (b CommandBlock) Edited(e CommandBlockEdit) CommandBlock {
	b.Mode = e.Mode
	b.Conditional, b.NeedsRedstone = e.Conditional, e.NeedsRedstone
	b.Command, b.CustomName, b.TrackOutput = e.Command, e.Name, e.TrackOutput
	b.TickDelay, b.ExecuteOnFirstTick = e.TickDelay, e.ExecuteOnFirstTick
	if !b.TrackOutput {
		b.LastOutput = ""
	}
	return b
}
