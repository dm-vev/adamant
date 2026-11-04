package session

import (
	"strings"

	"github.com/df-mc/dragonfly/protocol/bedrock/internal/bedrockdata"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

// knowsEnumOption reports whether the clients of the tables know an option
// of a command enum: of the registry enums, Block and Item, not a block
// their protocol lacks, as 2193 lacks the disc press, which BDS of it does
// not list; any other option.
func (w *wireTables) knowsEnumOption(enum, option string) bool {
	if enum != "Block" && enum != "Item" {
		return true
	}
	if !strings.Contains(option, ":") {
		option = "minecraft:" + option
	}
	return bedrockdata.KnowsBlock(w.proto.Number, option)
}

// dropUnknownEnumOptions removes from a packet the options of its enums
// the clients of the tables do not know, with the values and constraints
// only those options had. The packet is then the one BuildAvailableCommands
// builds from enums without those options, its values in the same order.
func (w *wireTables) dropUnknownEnumOptions(pk *mc.AvailableCommandsPacket) {
	type option struct{ enum, value uint32 }
	var dropped map[option]bool
	for i := range pk.Enums {
		e := &pk.Enums[i]
		indices := e.ValueIndices[:0]
		for _, index := range e.ValueIndices {
			if w.knowsEnumOption(e.Type, pk.EnumValues[index]) {
				indices = append(indices, index)
				continue
			}
			if dropped == nil {
				dropped = make(map[option]bool)
			}
			dropped[option{uint32(i), index}] = true
		}
		e.ValueIndices = indices
	}
	if dropped == nil {
		return
	}
	used := make([]bool, len(pk.EnumValues))
	for _, e := range pk.Enums {
		for _, index := range e.ValueIndices {
			used[index] = true
		}
	}
	remap := make([]uint32, len(pk.EnumValues))
	values := pk.EnumValues[:0]
	for index, value := range pk.EnumValues {
		if used[index] {
			remap[index] = uint32(len(values))
			values = append(values, value)
		}
	}
	pk.EnumValues = values
	for _, e := range pk.Enums {
		for j, index := range e.ValueIndices {
			e.ValueIndices[j] = remap[index]
		}
	}
	constraints := pk.Constraints[:0]
	for _, c := range pk.Constraints {
		if !dropped[option{c.EnumIndex, c.EnumValueIndex}] {
			c.EnumValueIndex = remap[c.EnumValueIndex]
			constraints = append(constraints, c)
		}
	}
	pk.Constraints = constraints
}
