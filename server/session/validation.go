package session

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/block/cube"
)

// parseBlockFace validates client-supplied faces to avoid panics in block logic.
func parseBlockFace(face int32) (cube.Face, error) {
	if face < int32(cube.FaceDown) || face > int32(cube.FaceEast) {
		return 0, fmt.Errorf("invalid block face %d", face)
	}
	return cube.Face(face), nil
}
