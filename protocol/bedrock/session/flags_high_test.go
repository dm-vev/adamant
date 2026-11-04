package session

import (
	"testing"

	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
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
	t.Parallel()
	m := mc.NewMetadata()
	(&Session{}).addSpecificMetadata(highFlagEntity{}, m)
	two := m[mc.EntityDataKeyFlagsTwo].(int64)
	for name, flag := range map[string]uint8{"ram attack": mc.EntityDataFlagRamAttack, "croaking": mc.EntityDataFlagCroaking,
		"playing dead": mc.EntityDataFlagPlayingDead, "jump goal": mc.EntityDataFlagJumpGoal} {
		if two&(1<<(flag-64)) == 0 {
			t.Errorf("%v flag missing from the second flag field", name)
		}
	}
}
