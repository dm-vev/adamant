package entity

import "github.com/df-mc/dragonfly/server/world"

// NewRaidSpawner constructs one registered Java raider with its wave and
// leadership state installed before the handle is published to the world.
func newRaidSpawner(opts world.EntitySpawnOpts, id string, raidID int64, wave int, leader bool) *world.EntityHandle {
	switch id {
	case evokerType.id:
		return opts.New(evokerType, hostileConfig{kind: id, raidID: raidID, raidWave: wave, raidLeader: leader})
	case pillagerType.id:
		return opts.New(pillagerType, hostileConfig{kind: id, raidID: raidID, raidWave: wave, raidLeader: leader})
	case vindicatorType.id:
		return opts.New(vindicatorType, hostileConfig{kind: id, raidID: raidID, raidWave: wave, raidLeader: leader})
	case ravagerType.id:
		return opts.New(ravagerType, hostileConfig{kind: id, raidID: raidID, raidWave: wave, raidLeader: leader})
	case entityWitchType.EncodeEntity():
		return opts.New(entityWitchType, witchConfig{RaidID: raidID, RaidWave: wave, RaidLeader: leader})
	default:
		return nil
	}
}
