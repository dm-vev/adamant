package player

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/enchantment"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestMeleeHitStopsUpdatingRemovedTarget(t *testing.T) {
	for _, lethal := range []bool{false, true} {
		for _, critical := range []bool{false, true} {
			name := "surviving"
			if lethal {
				name = "lethal"
			}
			if critical {
				name += " critical"
			}
			t.Run(name, func(t *testing.T) {
				var logs bytes.Buffer
				conn := &releaseTestConn{}
				s := session.Config{Log: slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})), MaxChunkRadius: 1}.New(conn)
				t.Cleanup(s.CloseConnection)
				w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry}.New()
				t.Cleanup(func() { _ = w.Close() })
				pos := mgl64.Vec3{8.5, 64, 8.5}
				var loader *world.Loader
				<-w.Exec(func(tx *world.Tx) {
					tx.SetBlock(cube.Pos{8, 63, 8}, block.Stone{}, nil)
					loader = world.NewLoader(1, w, meleeTargetViewer{session: s})
					loader.Move(tx, pos)
					loader.Load(tx, 1)
				})
				<-w.Exec(func(tx *world.Tx) {
					defer loader.Close(tx)
					cfg := Config{Position: pos}
					if critical {
						cfg.FallDistance = 1
					}
					p := tx.AddEntity(world.EntitySpawnOpts{Position: pos}.New(Type, cfg)).(*Player)
					p.SetHeldItems(item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1).WithEnchantments(item.NewEnchantment(enchantment.FireAspect, 1)), item.Stack{})
					cow := tx.AddEntity(entity.NewCow(world.EntitySpawnOpts{Position: pos.Add(mgl64.Vec3{0, 0, 1})})).(*entity.Cow)
					if lethal {
						cow.SetMaxHealth(1)
					} else {
						cow.SetMaxHealth(100)
						cow.Heal(100, entity.FoodHealingSource{})
					}
					if !p.AttackEntity(cow) {
						t.Fatal("attack was rejected")
					}
					held, _ := p.HeldItems()
					if held.Durability() != held.MaxDurability()-1 {
						t.Errorf("sword durability = %d, want %d", held.Durability(), held.MaxDurability()-1)
					}
					if cow.H().Closed() != lethal {
						t.Fatalf("target removed = %v, want %v", cow.H().Closed(), lethal)
					}
					if !lethal && cow.OnFireDuration() == 0 {
						t.Error("surviving target did not receive Fire Aspect")
					}
				})
				const marker = "melee-lifecycle-done"
				s.SendMessage(marker)
				conn.waitForMessage(t, marker)
				if strings.Contains(logs.String(), "entity runtime ID not found") {
					t.Errorf("attack used a removed entity: %s", &logs)
				}
				conn.mu.Lock()
				defer conn.mu.Unlock()
				criticals, deaths := 0, 0
				for _, pk := range conn.packets {
					switch pk := pk.(type) {
					case *packet.Animate:
						if pk.EntityRuntimeID == 0 {
							t.Error("animation targets runtime ID zero")
						}
						if pk.ActionType == packet.AnimateActionCriticalHit {
							criticals++
						}
					case *packet.SetActorData:
						if pk.EntityRuntimeID == 0 {
							t.Error("metadata targets runtime ID zero")
						}
					case *packet.ActorEvent:
						if pk.EventType == packet.ActorEventDeath {
							deaths++
						}
					}
				}
				if !lethal && critical && criticals != 1 {
					t.Errorf("surviving critical-hit animations = %d, want 1", criticals)
				}
				if lethal && deaths != 1 {
					t.Errorf("death animations = %d, want 1", deaths)
				}
			})
		}
	}
}

// Forward mob updates to a real session while keeping the attacking player
// separate from the observing client.
type meleeTargetViewer struct {
	world.NopViewer
	session *session.Session
}

func (v meleeTargetViewer) ViewEntity(e world.Entity) {
	if e.H().Type() != Type {
		v.session.ViewEntity(e)
	}
}
func (v meleeTargetViewer) HideEntity(e world.Entity) {
	if e.H().Type() != Type {
		v.session.HideEntity(e)
	}
}
func (v meleeTargetViewer) ViewEntityState(e world.Entity) {
	if e.H().Type() != Type {
		v.session.ViewEntityState(e)
	}
}
func (v meleeTargetViewer) ViewEntityAction(e world.Entity, action world.EntityAction) {
	if e.H().Type() != Type {
		v.session.ViewEntityAction(e, action)
	}
}
