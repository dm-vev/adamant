package cmd

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// ExecutionContext describes Java execute-command state without changing the
// transaction that owns the command's world operations.
type ExecutionContext struct {
	Executor world.Entity
	Position mgl64.Vec3
	Rotation cube.Rotation
	World    *world.World
	Anchor   Anchor
}

// Anchor identifies the point on an executor used by position-relative commands.
type Anchor uint8

const (
	Feet Anchor = iota
	Eyes
)

// ContextSource preserves the original command source while exposing a modified
// execution context to nested commands.
type ContextSource struct {
	Source
	Context ExecutionContext
}

// WithContext returns a source that forwards output to src and carries context.
func WithContext(src Source, context ExecutionContext) ContextSource {
	return ContextSource{Source: src, Context: context}
}

// ExecutionContextOf returns the modified context carried by src, if any.
func ExecutionContextOf(src Source) (ExecutionContext, bool) {
	context, ok := src.(interface{ CommandExecutionContext() ExecutionContext })
	if !ok {
		return ExecutionContext{}, false
	}
	return context.CommandExecutionContext(), true
}

// CommandExecutionContext implements the context accessor used by nested commands.
func (s ContextSource) CommandExecutionContext() ExecutionContext { return s.Context }

// Position returns the execution position, making selector-relative consumers
// see the current execute position while retaining the original source.
func (s ContextSource) Position() mgl64.Vec3 { return s.Context.Position }
