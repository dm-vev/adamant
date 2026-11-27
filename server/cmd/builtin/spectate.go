package builtin

import (
	"github.com/dm-vev/adamant/server/cmd"
	"github.com/dm-vev/adamant/server/player"
	"github.com/dm-vev/adamant/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// spectateCommand ports SpectateCommand: a player in spectator mode watches
// through the eyes of an entity, or stops doing so without a target.
type spectateCommand struct {
	worldOpCommand
	Target cmd.Optional[cmd.OneEntity] `cmd:"target"`
	Player cmd.Optional[cmd.OnePlayer] `cmd:"player"`
}

func registerSpectateCommand(guard worldOpCommand) {
	cmd.Register(newCommand("spectate", nil, spectateCommand{worldOpCommand: guard}))
}

type spectateTargetInfo struct {
	ok    bool
	world *world.World
	pos   mgl64.Vec3
	name  string
}

type spectateOutcome struct {
	notPlayer     bool
	selfErr       bool
	notSpectator  string
	missingTarget bool
	ok            bool
}

// Run ...
func (c spectateCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	playerSel, hasPlayer := c.Player.Load()
	var specSel cmd.Selector
	if hasPlayer {
		specSel = playerSel
	} else {
		specSel = optionalEntities(cmd.Optional[cmd.Entities]{}, src)
	}
	targetSel, hasTarget := c.Target.Load()
	if !hasTarget {
		cmd.ForEach(src, o, tx, specSel,
			func(_ *world.Tx, t cmd.Target) spectateOutcome {
				p, ok := t.(*player.Player)
				if !ok {
					return spectateOutcome{notPlayer: true}
				}
				if p.GameMode() != world.GameModeSpectator {
					return spectateOutcome{notSpectator: p.Name()}
				}
				p.Spectate(nil)
				return spectateOutcome{ok: true}
			},
			func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[spectateOutcome]) {
				reportSpectateResult(o, hasPlayer, "", rs)
			},
		)
		return
	}
	cmd.ForEach(src, o, tx, targetSel,
		func(rtx *world.Tx, t cmd.Target) spectateTargetInfo {
			e, ok := t.(world.Entity)
			if !ok {
				return spectateTargetInfo{}
			}
			var w *world.World
			if rtx != nil {
				w = rtx.World()
			}
			return spectateTargetInfo{ok: true, world: w, pos: e.Position(), name: commandTargetName(e)}
		},
		func(src cmd.Source, o *cmd.Output, tx *world.Tx, rs []cmd.Result[spectateTargetInfo]) {
			if len(rs) != 1 || !rs[0].Value.ok || rs[0].Handle == nil {
				o.Errort(cmd.MessageNoTargets)
				return
			}
			info := rs[0].Value
			targetHandle := rs[0].Handle
			cmd.ForEach(src, o, tx, specSel,
				func(ptx *world.Tx, t cmd.Target) spectateOutcome {
					p, ok := t.(*player.Player)
					if !ok {
						return spectateOutcome{notPlayer: true}
					}
					if targetHandle == p.H() {
						return spectateOutcome{selfErr: true}
					}
					if p.GameMode() != world.GameModeSpectator {
						return spectateOutcome{notSpectator: p.Name()}
					}
					if ptx != nil && ptx.World() == info.world {
						ent, ok := targetHandle.Entity(ptx)
						if !ok {
							return spectateOutcome{missingTarget: true}
						}
						p.Spectate(ent)
						return spectateOutcome{ok: true}
					}
					pHandle := p.H()
					p.TeleportToWorld(ptx, info.world, info.pos)
					info.world.Do(func(dtx *world.Tx) {
						np, ok := pHandle.Entity(dtx)
						if !ok {
							return
						}
						if ent, ok := targetHandle.Entity(dtx); ok {
							np.(*player.Player).Spectate(ent)
						}
					})
					return spectateOutcome{ok: true}
				},
				func(_ cmd.Source, o *cmd.Output, _ *world.Tx, rs []cmd.Result[spectateOutcome]) {
					reportSpectateResult(o, hasPlayer, info.name, rs)
				},
			)
		},
	)
}

func reportSpectateResult(o *cmd.Output, hasPlayer bool, targetName string, rs []cmd.Result[spectateOutcome]) {
	if len(rs) != 1 {
		if hasPlayer {
			o.Error(cmd.ErrNoPlayerFound)
		} else {
			o.Errorf("A player is required to run this command here")
		}
		return
	}
	res := rs[0].Value
	switch {
	case res.notPlayer:
		if hasPlayer {
			o.Errorf("Only players may be affected by this command, but the provided selector includes entities")
		} else {
			o.Errorf("A player is required to run this command here")
		}
	case res.selfErr:
		o.Errorf("You can't spectate yourself")
	case res.notSpectator != "":
		o.Errorf("%s is not in spectator mode", res.notSpectator)
	case res.missingTarget:
		o.Errort(cmd.MessageNoTargets)
	case targetName == "":
		o.Printf("No longer spectating an entity")
	default:
		o.Printf("Now spectating %s", targetName)
	}
}
