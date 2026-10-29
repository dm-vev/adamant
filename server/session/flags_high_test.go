package session

import (
	"testing"

	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
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
	m := v2193.NewMetadata()
	(&Session{}).addSpecificMetadata(highFlagEntity{}, m)
	two := m[v2193.EntityDataKeyFlagsTwo].(int64)
	for name, flag := range map[string]uint8{"ram attack": v2193.EntityDataFlagRamAttack, "croaking": v2193.EntityDataFlagCroaking,
		"playing dead": v2193.EntityDataFlagPlayingDead, "jump goal": v2193.EntityDataFlagJumpGoal} {
		if two&(1<<(flag-64)) == 0 {
			t.Errorf("%v flag missing from the second flag field", name)
		}
	}
}
