package cmd

import (
	"strings"
	"sync"

	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// BlockSource executes commands as a command block at its block position. It
// implements Deferrer so command blocks can record late output from
// cross-world commands.
type BlockSource struct {
	pos        cube.Pos
	world      *world.World
	name       string
	mu         sync.Mutex
	output     *Output
	deferred   bool
	pending    int
	onDeferred func(o *Output, text string)
}

// NewBlockSource constructs a source for command-block execution.
func NewBlockSource(pos cube.Pos, w *world.World, name string) *BlockSource {
	return &BlockSource{pos: pos, world: w, name: name}
}
func (s *BlockSource) Position() mgl64.Vec3 { return s.pos.Vec3Centre() }
func (s *BlockSource) Name() string {
	if s.name != "" {
		return s.name
	}
	return "@"
}
func (s *BlockSource) SendCommandOutput(o *Output) {
	s.mu.Lock()
	s.output = o
	s.mu.Unlock()
}

// OnDeferred registers fn to run once a deferred command completes (or is
// aborted, in which case o is nil). If the command completes synchronously
// without calling Defer, fn is not called.
func (s *BlockSource) OnDeferred(fn func(o *Output, text string)) {
	s.mu.Lock()
	s.onDeferred = fn
	s.mu.Unlock()
}

// Defer implements Deferrer. When all pending deferred deliveries finish, any
// callback registered with OnDeferred is invoked with the captured output.
func (s *BlockSource) Defer() func() {
	s.mu.Lock()
	s.deferred = true
	s.pending++
	s.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			s.pending--
			done := s.pending == 0
			cb := s.onDeferred
			o := s.output
			s.mu.Unlock()
			if done && cb != nil {
				cb(o, formatBlockOutput(o))
			}
		})
	}
}

// Deferred reports whether the command executed with s deferred its output.
func (s *BlockSource) Deferred() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deferred
}

// PermissionLevel returns the level command blocks run commands at, as the
// server sets it: LevelGameMaster by default, as Java fixes it.
func (s *BlockSource) PermissionLevel() Level { return CommandBlockLevel() }

// Output returns the result captured from the last executed command.
func (s *BlockSource) Output() *Output {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.output == nil {
		return &Output{}
	}
	return s.output
}

// Text renders the output as plain text for the command block actor.
func (s *BlockSource) Text() string {
	return formatBlockOutput(s.Output())
}

func formatBlockOutput(o *Output) string {
	if o == nil {
		return ""
	}
	var text strings.Builder
	for _, message := range o.Messages() {
		if text.Len() > 0 {
			text.WriteByte('\n')
		}
		text.WriteString(message.String())
	}
	for _, err := range o.Errors() {
		if text.Len() > 0 {
			text.WriteByte('\n')
		}
		text.WriteString(err.Error())
	}
	return text.String()
}

// World returns the world containing the source.
func (s *BlockSource) World() *world.World { return s.world }

// BlockPosition returns the command block's position.
func (s *BlockSource) BlockPosition() cube.Pos { return s.pos }
