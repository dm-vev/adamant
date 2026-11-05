package builtin

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
)

// dataCaptureLines are the lines /data is captured with: get, merge and remove
// on storage, entities and blocks, with the arguments that work and those that
// fail.
var dataCaptureLines = []string{
	"",
	"get",
	"get nothing",
	"get nothing x",
	"bogus storage st",
	"bogus entity @e[type=zombie]",
	"bogus block 3 64 3",
	"bogus block 0 64 0",
	// storage.
	"get storage",
	"get storage st",
	"get storage new",
	"get storage st extra words",
	"remove storage",
	"remove storage st",
	"remove storage new",
	"remove storage st extra words",
	"merge storage",
	"merge storage st",
	"merge storage st {}",
	`merge storage st {"x":1}`,
	`merge storage new {"x":1}`,
	`merge storage st {"a":2}`,
	`merge storage st {"a": 2, "b": [1, 2], "c": {"d": "e f"}}`,
	"merge storage st {bad",
	"merge storage st 5",
	"merge storage st null",
	`merge storage st extra {"x":1}`,
	"set storage st",
	`set storage st {"x":1}`,
	// entity.
	"get entity",
	"get entity @e[type=zombie]",
	"get entity @e[type=zombie] Health",
	"get entity @e[type=zombie] Pos",
	"get entity @e[type=zombie] id",
	"get entity @e[type=zombie] nothing",
	"get entity @e[type=zombie] id.nothing",
	"get entity @e[type=nothing]",
	"get entity @e[bad=1]",
	"get entity Nobody",
	"get entity @s",
	"get entity @s Health",
	"get entity @e[distance=..20,sort=nearest,limit=1]",
	"get entity @e[type=cow,sort=furthest,limit=1] Pos",
	`merge entity @e[type=zombie] {"Health":5}`,
	`merge entity @e[type=zombie] {"CustomName":"Named","CustomNameVisible":true}`,
	`merge entity @e[type=cow] {"CustomName":"Both"}`,
	`merge entity @e {"CustomName":"All"}`,
	`merge entity @e[type=zombie] {"Pos":[1.5,70,2.5]}`,
	"merge entity @e[type=zombie] {bad",
	"merge entity @e[type=zombie] 5",
	"merge entity @e[type=zombie]",
	"merge entity @e[type=nothing] {}",
	"merge entity @e[bad=1] {}",
	"remove entity @e[type=zombie] CustomName",
	"remove entity @e[type=zombie] Health",
	"remove entity @e[type=zombie] nothing",
	"remove entity @e[type=cow] CustomName",
	"remove entity @e[type=zombie]",
	"remove entity @e[type=nothing] x",
	"set entity @e[type=zombie] x",
	"set entity @e[type=nothing] x",
	// block.
	"get block 3 64 3",
	"get block 3 64 3 CustomName",
	"get block 3 64 3 Items",
	"get block 3 64 3 id",
	"get block 3 64 3 nothing",
	"get block 3 64 3 id extra",
	"get block 0 64 0",
	"get block 0 64 0 id",
	"get block ~ ~ ~",
	"get block 3 64 3 ~",
	"get block a b c",
	"get block 3 64",
	"get block",
	`merge block 3 64 3 {"CustomName":"Treasury"}`,
	`merge block 3 64 3 {"CustomName": "Two Words"}`,
	`merge block 3 64 3 {"Items":[]}`,
	`merge block 3 64 3 {"nothing":1}`,
	"merge block 3 64 3 {bad",
	"merge block 3 64 3 5",
	"merge block 3 64 3",
	`merge block 0 64 0 {"x":1}`,
	`merge block 0 64 0`,
	`merge block a b c {"x":1}`,
	"remove block 3 64 3 CustomName",
	"remove block 3 64 3 nothing",
	"remove block 3 64 3 Items",
	"remove block 3 64 3",
	"remove block 0 64 0 x",
	"remove block 0 64 0",
	"set block 3 64 3 x",
	"set block 0 64 0 x",
}

// dataViaTables is /data run through the tables of its targets and actions.
type dataViaTables struct {
	gameMaster
	Args cmd.Varargs
}

func (c dataViaTables) Run(src cmd.Source, out *cmd.Output, tx *world.Tx) {
	dataCommand{Args: c.Args}.runTables(src, out, tx)
}

// TestDataTablesMatchLegacy requires the tables of targets and actions to do
// exactly what /data did before them, on every line of the capture: the same
// output text and the same effects.
func TestDataTablesMatchLegacy(t *testing.T) {
	legacy := runCapture(t, dataCaptureLines, "data", dataCommand{}, dataCaptureEffects)
	tables := runCapture(t, dataCaptureLines, "data", dataViaTables{}, dataCaptureEffects)
	if legacy != tables {
		checkEqualCaptures(t, legacy, tables)
	}
}

// dataCaptureEffects writes down what the data of the chest and the entities
// of the overworld are after a case.
func dataCaptureEffects(c *captureWorlds, tx *world.Tx) []string {
	var lines []string
	if chest, ok := tx.Block(cube.Pos{3, 64, 3}).(world.NBTer); ok {
		encoded, _ := json.Marshal(chest.EncodeNBT())
		lines = append(lines, "chest "+string(encoded))
	}
	for e := range tx.Entities() {
		encoded, err := json.Marshal(e.H().NBT(tx))
		lines = append(lines, c.text(fmt.Sprintf("nbt %s %s %v", c.entityLabel(e), encoded, err)))
	}
	slices.Sort(lines)
	return lines
}

// TestDataCapture writes down what /data does for every line of
// dataCaptureLines: the output of the command and its effects.
func TestDataCapture(t *testing.T) {
	checkGolden(t, "data_capture.golden", runCapture(t, dataCaptureLines, "data", dataCommand{}, dataCaptureEffects))
}
