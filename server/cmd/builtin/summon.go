package builtin

import (
	"strings"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
)

type summonCommand struct {
	Entity string               `cmd:"entity"`
	Count  cmd.Optional[uint16] `cmd:"count"`
}

func newSummonCommand() cmd.Command {
	return cmd.New("summon", "Spawns an entity at the command source.", nil, summonCommand{})
}

func (c summonCommand) Run(src cmd.Source, o *cmd.Output, tx *world.Tx) {
	if tx == nil || tx.World() == nil {
		o.Error("world unavailable")
		return
	}

	id := strings.ToLower(strings.TrimSpace(c.Entity))
	if id == "" {
		o.Errort(cmd.MessageUsage, "/summon <entity> [count]")
		return
	}
	if !strings.Contains(id, ":") {
		id = "minecraft:" + id
	}

	count := uint16(1)
	if v, ok := c.Count.Load(); ok {
		if v == 0 {
			o.Error("count must be at least 1")
			return
		}
		count = v
	}
	if count > 128 {
		o.Error("count too large (max 128)")
		return
	}

	pos := src.Position()
	if _, ok := src.(world.Entity); !ok {
		pos = tx.World().Spawn().Vec3Middle()
	}

	switch id {
	case "minecraft:zombie":
		for i := uint16(0); i < count; i++ {
			tx.AddEntity(entity.NewZombie(world.EntitySpawnOpts{Position: pos}))
		}
	default:
		o.Errorf("unsupported entity: %s", id)
		return
	}

	o.Printf("Summoned %d %s", count, id)
}
