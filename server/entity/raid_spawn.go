package entity

import "github.com/df-mc/dragonfly/server/world"

// NewRaidSpawner constructs one registered Java raider with its wave and
// leadership state installed before the handle is published to the world.
func NewRaidSpawner(opts world.EntitySpawnOpts, id string, raidID int64, wave int, leader bool) *world.EntityHandle {
	switch id {
	case EvokerType.id:
		return opts.New(EvokerType, hostileConfig{kind: id, raidID: raidID, raidWave: wave, raidLeader: leader})
	case PillagerType.id:
		return opts.New(PillagerType, hostileConfig{kind: id, raidID: raidID, raidWave: wave, raidLeader: leader})
	case VindicatorType.id:
		return opts.New(VindicatorType, hostileConfig{kind: id, raidID: raidID, raidWave: wave, raidLeader: leader})
	case RavagerType.id:
		return opts.New(RavagerType, hostileConfig{kind: id, raidID: raidID, raidWave: wave, raidLeader: leader})
	case WitchType.EncodeEntity():
		return opts.New(WitchType, WitchConfig{RaidID: raidID, RaidWave: wave, RaidLeader: leader})
	default:
		return nil
	}
}
