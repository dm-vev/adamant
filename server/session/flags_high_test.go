package session

import (
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
)

type highFlagEntity struct{}

func (highFlagEntity) RamAttacking() bool    { return true }
func (highFlagEntity) Croaking() bool        { return true }
func (highFlagEntity) PlayingDead() bool     { return true }
func (highFlagEntity) JumpGoalJumping() bool { return true }

// TestHighEntityFlagsReachFlagsTwo checks that flags from 64 up, such as the
// ram attack of goats and the croak of frogs, go into the second flag field
// instead of being shifted out of the first.
func TestHighEntityFlagsReachFlagsTwo(t *testing.T) {
	m := protocol.NewEntityMetadata()
	(&Session{}).addSpecificMetadata(highFlagEntity{}, m)
	two := m[protocol.EntityDataKeyFlagsTwo].(int64)
	for name, flag := range map[string]uint8{"ram attack": protocol.EntityDataFlagRamAttack, "croaking": protocol.EntityDataFlagCroaking,
		"playing dead": protocol.EntityDataFlagPlayingDead, "jump goal": protocol.EntityDataFlagJumpGoal} {
		if two&(1<<(flag-64)) == 0 {
			t.Errorf("%v flag missing from the second flag field", name)
		}
	}
}
