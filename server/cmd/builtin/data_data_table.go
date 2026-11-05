package builtin

import (
	"encoding/json"
	"strings"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
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

// runTables runs the command through the tables of its targets and actions.
func (c dataCommand) runTables(src cmd.Source, out *cmd.Output, tx *world.Tx) {
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
	s := dataStorageRef{namespace: "java:storage:" + c.words[2], key: strings.Join(c.words[2:], " ")}
	s.data, s.err = c.tx.World().LoadWorldData(s.namespace)
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

// dataEntityActions are the actions of /data entity by keyword. They work on
// one entity, and report whether the command goes on with the next.
var dataEntityActions = map[string]func(c *dataCall, e world.Entity) bool{
	"get":    dataEntityGet,
	"merge":  dataEntityMerge,
	"remove": dataEntityRemove,
}

// dataEntity is `entity <targets>`.
func dataEntity(c *dataCall) {
	if len(c.words) < 3 {
		c.out.Error("data entity requires a target")
		return
	}
	targets, err := cmd.ResolveTargets(c.words[2], c.src, c.tx)
	if err != nil {
		c.out.Error(err)
		return
	}
	var entities []world.Entity
	for _, t := range targets {
		if e, ok := t.(world.Entity); ok {
			entities = append(entities, e)
		}
	}
	if len(entities) == 0 {
		c.out.Errort(cmd.MessageNoTargets)
		return
	}
	action, ok := dataEntityActions[c.action]
	if !ok {
		c.out.Error("unsupported entity data action")
		return
	}
	for _, e := range entities {
		if !action(c, e) {
			return
		}
	}
	c.out.Printf("Updated data for %d entity(s)", len(entities))
}

// dataEntityGet is `get entity <targets> [<path>]`.
func dataEntityGet(c *dataCall, e world.Entity) bool {
	tag := e.H().NBT(c.tx)
	if len(c.words) > 3 {
		v, exists := nbtPath(tag, c.words[3])
		if !exists {
			c.out.Error("missing data path " + c.words[3])
			return false
		}
		c.out.SetResult(dataResult(v))
		c.out.Printf("%s", formatData(v))
		return true
	}
	c.out.SetResult(dataResult(tag))
	c.out.Printf("%s", formatData(tag))
	return true
}

// dataEntityMerge is `merge entity <targets> <json>`.
func dataEntityMerge(c *dataCall, e world.Entity) bool {
	if len(c.words) <= 3 {
		c.out.Error("unsupported entity data action")
		return false
	}
	var patch map[string]any
	if err := json.Unmarshal([]byte(strings.Join(c.words[3:], " ")), &patch); err != nil {
		c.out.Error(err)
		return false
	}
	if err := e.H().MergeNBT(c.tx, patch); err != nil {
		c.out.Error(err)
		return false
	}
	return true
}

// dataEntityRemove is `remove entity <targets> <path>`.
func dataEntityRemove(c *dataCall, e world.Entity) bool {
	if len(c.words) <= 3 {
		c.out.Error("unsupported entity data action")
		return false
	}
	if err := e.H().MergeNBT(c.tx, map[string]any{c.words[3]: nil}); err != nil {
		c.out.Error(err)
		return false
	}
	return true
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
