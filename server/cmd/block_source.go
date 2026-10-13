package cmd

import (
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// BlockSource executes commands as a command block at its block position.
type BlockSource struct {
	pos    cube.Pos
	world  *world.World
	name   string
	output *Output
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
func (s *BlockSource) SendCommandOutput(o *Output) { s.output = o }

// Output returns the result captured from the last executed command.
func (s *BlockSource) Output() *Output {
	if s.output == nil {
		return &Output{}
	}
	return s.output
}

// Text renders the output as plain text for the command block actor.
func (s *BlockSource) Text() string {
	o := s.Output()
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
