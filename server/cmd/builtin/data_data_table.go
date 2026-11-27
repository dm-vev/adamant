package builtin

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/df-mc/goleveldb/leveldb"
	"github.com/dm-vev/adamant/server/block/cube"
	"github.com/dm-vev/adamant/server/cmd"
	"github.com/dm-vev/adamant/server/world"
)

// dataCall is a /data command as the functions of its targets and actions work
// on it: the words of the command, the first of which is the action and the
// second the target.
type dataCall struct {
	action string
	words  []string
	src    cmd.Source
	out    *cmd.Output
	tx     *world.Tx
}

// dataTargets are the targets of /data by keyword. Each reports on its own
// what it cannot do.
var dataTargets = map[string]func(c *dataCall){
	"storage": dataStorage,
	"entity":  dataEntity,
	"block":   dataBlock,
}

// Run runs the command through the tables of its targets and actions.
func (c dataCommand) Run(src cmd.Source, out *cmd.Output, tx *world.Tx) {
	if tx == nil {
		out.Error("data requires a world")
		return
	}
	words := strings.Fields(string(c.Args))
	if len(words) < 2 {
		out.Error("data requires get, merge, or remove and a target")
		return
	}
	target, ok := dataTargets[words[1]]
	if !ok {
		out.Error("unsupported data target")
		return
	}
	target(&dataCall{action: words[0], words: words, src: src, out: out, tx: tx})
}

// dataStorageRef is the storage a /data storage command names, with the data
// that is stored in it, or the error of loading it.
type dataStorageRef struct {
	namespace string
	key       string
	data      []byte
	err       error
}

// dataStorageActions are the actions of /data storage by keyword.
var dataStorageActions = map[string]func(c *dataCall, s dataStorageRef){
	"get":    dataStorageGet,
	"remove": dataStorageRemove,
	"merge":  dataStorageMerge,
}

// dataStorage is `storage <key>`.
func dataStorage(c *dataCall) {
	if len(c.words) < 3 {
		c.out.Error("data storage requires a key")
		return
	}
	s := dataStorageRef{namespace: "cmd:storage:" + c.words[2], key: strings.Join(c.words[2:], " ")}
	s.data, s.err = c.tx.World().LoadWorldData(s.namespace)
	if errors.Is(s.err, leveldb.ErrNotFound) {
		s.data, s.err = c.tx.World().LoadWorldData("java:storage:" + c.words[2])
	}
	action, ok := dataStorageActions[c.action]
	if !ok {
		c.out.Error("unsupported storage operation")
		return
	}
	action(c, s)
}

// dataStorageGet is `get storage <key>`: the stored data as it is.
func dataStorageGet(c *dataCall, s dataStorageRef) {
	if s.err != nil {
		c.out.Error(s.err)
		return
	}
	c.out.Printf("%s", string(s.data))
}

// dataStorageRemove is `remove storage <key>`.
func dataStorageRemove(c *dataCall, s dataStorageRef) {
	if err := c.tx.World().SaveWorldData(s.namespace, nil); err != nil {
		c.out.Error(err)
		return
	}
	c.out.Printf("Removed storage %s", s.key)
}

// dataStorageMerge is `merge storage <key> <json>`: the fields of the JSON
// object replace those stored.
func dataStorageMerge(c *dataCall, s dataStorageRef) {
	if len(c.words) < 4 {
		c.out.Error("unsupported storage operation")
		return
	}
	var current, patch map[string]any
	if s.err == nil {
		if err := json.Unmarshal(s.data, &current); err != nil {
			c.out.Error(err)
			return
		}
	}
	if current == nil {
		current = make(map[string]any)
	}
	if err := json.Unmarshal([]byte(strings.Join(c.words[3:], " ")), &patch); err != nil {
		c.out.Error(err)
		return
	}
	for name, value := range patch {
		current[name] = value
	}
	encoded, err := json.Marshal(current)
	if err != nil {
		c.out.Error(err)
		return
	}
	if err := c.tx.World().SaveWorldData(s.namespace, encoded); err != nil {
		c.out.Error(err)
		return
	}
	c.out.Printf("Merged storage %s", s.key)
}

type dataEntityOutcome struct {
	isEntity  bool
	msg       string
	result    int
	hasResult bool
	errMsg    string
	err       error
}

// dataEntity is `entity <targets>`.
func dataEntity(c *dataCall) {
	if len(c.words) < 3 {
		c.out.Error("data entity requires a target")
		return
	}
	sel, err := cmd.ParseEntities(c.words[2], c.src)
	if err != nil {
		c.out.Error(err)
		return
	}
	action := c.action
	words := slices.Clone(c.words)
	var patch map[string]any
	var patchErr error
	if action == "merge" && len(words) > 3 {
		patchErr = json.Unmarshal([]byte(strings.Join(words[3:], " ")), &patch)
	}
	cmd.ForEach(c.src, c.out, c.tx, sel,
		func(rtx *world.Tx, t cmd.Target) dataEntityOutcome {
			e, ok := t.(world.Entity)
			if !ok {
				return dataEntityOutcome{}
			}
			return evalDataEntity(action, words, patch, patchErr, rtx, e)
		},
		func(_ cmd.Source, out *cmd.Output, _ *world.Tx, rs []cmd.Result[dataEntityOutcome]) {
			var matched []dataEntityOutcome
			for _, r := range rs {
				if r.Value.isEntity {
					matched = append(matched, r.Value)
				}
			}
			if len(matched) == 0 {
				out.Errort(cmd.MessageNoTargets)
				return
			}
			if action != "get" && action != "merge" && action != "remove" {
				out.Error("unsupported entity data action")
				return
			}
			for _, m := range matched {
				if m.err != nil {
					out.Error(m.err)
					return
				}
				if m.errMsg != "" {
					out.Error(m.errMsg)
					return
				}
				if m.hasResult {
					out.SetResult(m.result)
				}
				if m.msg != "" {
					out.Printf("%s", m.msg)
				}
			}
			out.Printf("Updated data for %d entity(s)", len(matched))
		},
	)
}

func evalDataEntity(action string, words []string, patch map[string]any, patchErr error, tx *world.Tx, e world.Entity) dataEntityOutcome {
	res := dataEntityOutcome{isEntity: true}
	switch action {
	case "get":
		tag := e.H().NBT(tx)
		if len(words) > 3 {
			v, exists := nbtPath(tag, words[3])
			if !exists {
				res.errMsg = "missing data path " + words[3]
				return res
			}
			res.result, res.hasResult, res.msg = dataResult(v), true, formatData(v)
			return res
		}
		res.result, res.hasResult, res.msg = dataResult(tag), true, formatData(tag)
		return res
	case "merge":
		if len(words) <= 3 {
			res.errMsg = "unsupported entity data action"
			return res
		}
		if patchErr != nil {
			res.err = patchErr
			return res
		}
		if err := e.H().MergeNBT(tx, patch); err != nil {
			res.err = err
		}
		return res
	case "remove":
		if len(words) <= 3 {
			res.errMsg = "unsupported entity data action"
			return res
		}
		if err := e.H().MergeNBT(tx, map[string]any{words[3]: nil}); err != nil {
			res.err = err
		}
		return res
	default:
		res.errMsg = "unsupported entity data action"
		return res
	}
}

// dataBlockCall is the block a /data block command names, as the data that it
// carries.
type dataBlockCall struct {
	pos   cube.Pos
	actor world.NBTer
	// path is the words after the coordinates.
	path []string
}

// dataBlockActions are the actions of /data block by keyword.
var dataBlockActions = map[string]func(c *dataCall, b dataBlockCall){
	"get":    dataBlockGet,
	"remove": dataBlockRemove,
	"merge":  dataBlockMerge,
}

// dataBlock is `block <x> <y> <z>`.
func dataBlock(c *dataCall) {
	if len(c.words) < 5 {
		c.out.Error("unsupported data target")
		return
	}
	vec, err := cmd.ResolveCoordinates(c.words[2:5], c.src)
	if err != nil {
		c.out.Error(err)
		return
	}
	pos := cube.PosFromVec3(vec)
	actor, ok := c.tx.Block(pos).(world.NBTer)
	if !ok {
		c.out.Error("block has no data")
		return
	}
	action, ok := dataBlockActions[c.action]
	if !ok {
		c.out.Error("unsupported data action")
		return
	}
	action(c, dataBlockCall{pos: pos, actor: actor, path: c.words[5:]})
}

// dataBlockGet is `get block <x> <y> <z> [<path>]`.
func dataBlockGet(c *dataCall, b dataBlockCall) {
	tag := b.actor.EncodeNBT()
	if len(b.path) > 0 {
		v, exists := nbtPath(tag, b.path[0])
		if !exists {
			c.out.Error("missing data path " + b.path[0])
			return
		}
		c.out.SetResult(dataResult(v))
		c.out.Printf("%s", formatData(v))
		return
	}
	c.out.SetResult(dataResult(tag))
	c.out.Printf("%s", formatData(tag))
}

// dataBlockRemove is `remove block <x> <y> <z> <path>`.
func dataBlockRemove(c *dataCall, b dataBlockCall) {
	if len(b.path) == 0 {
		c.out.Error("data remove requires a path")
		return
	}
	tag := b.actor.EncodeNBT()
	delete(tag, b.path[0])
	c.tx.SetBlockEntity(b.pos, b.actor.DecodeNBT(tag).(world.Block))
	c.out.Printf("Removed data from block at %s", b.pos)
}

// dataBlockMerge is `merge block <x> <y> <z> <json>`.
func dataBlockMerge(c *dataCall, b dataBlockCall) {
	if len(b.path) == 0 {
		c.out.Error("data merge requires a JSON object")
		return
	}
	var merge map[string]any
	if err := json.Unmarshal([]byte(strings.Join(b.path, " ")), &merge); err != nil {
		c.out.Error(err)
		return
	}
	tag := b.actor.EncodeNBT()
	for k, v := range merge {
		tag[k] = v
	}
	decoded, ok := b.actor.DecodeNBT(tag).(world.Block)
	if !ok {
		c.out.Error("block data cannot be merged")
		return
	}
	c.tx.SetBlockEntity(b.pos, decoded)
	c.out.Printf("Merged data into block at %s", b.pos)
}
