package player

import (
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/internal/txguard"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// sleepState is whether the player sleeps, where, and the timers of it.
type sleepState struct {
	sleeping      bool
	sleepPos      cube.Pos
	timeSinceRest int32
	sleepTimer    int
}

// sleepBegin records that the player lies down on the bed at the position
// passed, rested and with no ticks slept.
func (s *sleepState) sleepBegin(pos cube.Pos) {
	s.sleeping = true
	s.timeSinceRest = 0
	s.sleepTimer = 0
	s.sleepPos = pos
}

// sleepEnd records that the player left its bed. The timers stay for the
// player to read.
func (s *sleepState) sleepEnd() {
	s.sleeping = false
	s.sleepPos = cube.Pos{}
}

// sleepTick counts a tick in the timers: the ticks slept, up to 100, as
// Player.tick does, or the ticks awake.
func (s *sleepState) sleepTick() {
	if s.sleeping {
		s.timeSinceRest = 0
		// Player.tick counts the ticks slept up to 100.
		s.sleepTimer = min(s.sleepTimer+1, 100)
	} else if s.timeSinceRest < int32(^uint32(0)>>1) {
		s.timeSinceRest++
	}
}

// sleepingAt returns the position of the bed the player sleeps in and true,
// or false if it does not sleep.
func (s *sleepState) sleepingAt() (cube.Pos, bool) {
	if !s.sleeping {
		return cube.Pos{}, false
	}
	return s.sleepPos, true
}

// Sleep makes the player sleep at the given position. If the position does not map to a bed (specifically the head side),
// the player will not sleep.
func (p *Player) Sleep(pos cube.Pos) {
	if p.sleeping {
		// The player is already sleeping.
		return
	}

	if ok := txguard.Run(p.tx, func() {
		p.sleepInTx(p.tx, pos)
	}); ok {
		return
	}

	p.Do(func(tx *world.Tx, sleeper *Player) {
		if sleeper.sleeping {
			return
		}
		sleeper.sleepInTx(tx, pos)
	})
}

func (p *Player) sleepInTx(tx *world.Tx, pos cube.Pos) {
	b := tx.Block(pos)
	height := 0.5625
	switch bed := b.(type) {
	case block.Bed:
		if bed.Occupied {
			return
		}
	case block.StrawBed:
		var ok bool
		height, ok = bed.SleepHeight(pos, tx)
		if bed.Occupied || !ok {
			return
		}
	default:
		return
	}

	ctx, sendReminder := NewEventContext(p.tx, p), true
	if p.Handler().HandleSleep(ctx, &sendReminder); ctx.Cancelled() {
		return
	}
	entity.DismountRider(p, tx)

	switch bed := b.(type) {
	case block.Bed:
		bed.Sleeper, bed.Occupied = p.H(), true
		b = bed
	case block.StrawBed:
		bed.Sleeper, bed.Occupied = p.H(), true
		b = bed
	}

	tx.World().SetRequiredSleepDuration(time.Millisecond * 5050)

	p.setPosition(pos.Vec3Middle().Add(mgl64.Vec3{0, height + 0.125}))
	tx.SetBlock(pos, b, nil)
	p.sleepBegin(pos)
	p.SetVelocity(mgl64.Vec3{})
	p.data.Vel = mgl64.Vec3{}

	p.client().SendPlayerSpawn(pos.Vec3())

	if sendReminder {
		tx.BroadcastSleepingReminder(p)
	}

	tx.BroadcastSleepingIndicator()
	p.updateState()
}

// Wake forces the player out of bed if they are sleeping.
func (p *Player) Wake() {
	if !p.sleeping {
		return
	}

	if ok := txguard.Run(p.tx, func() {
		p.wakeInTx(p.tx)
	}); ok {
		return
	}

	p.Do(func(tx *world.Tx, sleeper *Player) {
		if !sleeper.sleeping {
			return
		}
		sleeper.wakeInTx(tx)
	})
}

func (p *Player) wakeInTx(tx *world.Tx) {
	if !p.sleeping {
		return
	}
	pos := p.sleepPos
	switch bed := tx.Block(pos).(type) {
	case block.Bed:
		bed.Sleeper, bed.Occupied = nil, false
		tx.SetBlock(pos, bed, nil)
		p.standUpFromBed(tx, pos, bed.Facing)
	case block.StrawBed:
		bed.Sleeper, bed.Occupied = nil, false
		tx.SetBlock(pos, bed, nil)
		bed.StopSleeping(pos, tx)
		p.standUpFromBed(tx, pos, bed.Facing)
	}
	p.sleepEnd()

	tx.BroadcastSleepingIndicator()

	viewers, release := p.viewers()
	for _, v := range viewers {
		v.ViewEntityWake(p)
	}
	releaseBorrowedViewers(tx, viewers, release)
	p.updateState()

}

// Sleeping returns true if the player is currently sleeping, along with the position of the bed the player is sleeping
// on.
func (p *Player) Sleeping() (cube.Pos, bool) {
	return p.sleepingAt()
}

// SleepTimer returns the ticks the player slept in its last sleep, up to
// 100, as Player.getSleepTimer.
func (p *Player) SleepTimer() int { return p.sleepTimer }

// TimeSinceRest returns awake ticks since the player last started sleeping.
func (p *Player) TimeSinceRest() int32 {
	return p.timeSinceRest
}
