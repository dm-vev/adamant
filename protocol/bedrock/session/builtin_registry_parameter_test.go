package session_test

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/protocol/bedrock/session"
	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/cmd/builtin"
	"github.com/df-mc/dragonfly/server/world"
	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

func TestRegistryArgumentsMatchBDSMetadata(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock_1_26_51_registry_arguments.json")
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		Parameters map[string]struct {
			Enum     string `json:"enum"`
			Options  byte   `json:"options"`
			Optional bool   `json:"optional"`
		} `json:"parameters"`
		Enums map[string]struct {
			Examples    []string          `json:"examples"`
			Constraints map[string][]byte `json:"constraints"`
		} `json:"enums"`
	}
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatal(err)
	}
	// Registering the builtin commands is for the whole process, and the
	// sessions of other tests would see them: run where none can.
	if isolated(t) {
		return
	}
	builtin.Register((*server.Server)(nil))
	commands := cmd.Commands()
	pk, _ := session.BuildAvailableCommands(commands, nil, make(map[string]struct{}))
	decoded, encoded := decodeRegistryCommands(t, pk)
	for _, tt := range []struct {
		command, parameter, native string
		count                      int
	}{
		{"give", "item", "give.itemName", 1},
		{"item", "item", "replaceitem.itemName", 1},
		{"setblock", "block", "setblock.tileName", 1},
		{"fill", "block", "fill.tileName", 2},
		{"fill", "filter", "fill.replaceTileName", 1},
	} {
		want := reference.Parameters[tt.native]
		count := 0
		for _, command := range decoded.Commands {
			if command.Name != tt.command {
				continue
			}
			for _, overload := range command.Overloads {
				for _, parameter := range overload.Parameters {
					if parameter.Name != tt.parameter {
						continue
					}
					count++
					if parameter.Type&(mc.CommandArgValid|mc.CommandArgEnum|mc.CommandArgSoftEnum) != mc.CommandArgValid|mc.CommandArgEnum {
						t.Fatalf("%s.%s is not a native hard enum: %#x", tt.command, tt.parameter, parameter.Type)
					}
					enum := decoded.Enums[parameter.Type&0xfffff]
					if enum.Type != want.Enum || parameter.Options != want.Options || parameter.Optional != want.Optional {
						t.Errorf("%s.%s = (%s, options %d, optional %v), BDS = %+v", tt.command, tt.parameter, enum.Type, parameter.Options, parameter.Optional, want)
					}
				}
			}
		}
		if count != tt.count {
			t.Errorf("%s.%s appears in %d overloads, want %d", tt.command, tt.parameter, count, tt.count)
		}
	}
	enums := make(map[string][]string)
	for _, enum := range decoded.Enums {
		if enum.Type != "Item" && enum.Type != "Block" {
			continue
		}
		if _, exists := enums[enum.Type]; exists {
			t.Fatalf("duplicate registry enum %s", enum.Type)
		}
		for _, index := range enum.ValueIndices {
			enums[enum.Type] = append(enums[enum.Type], decoded.EnumValues[index])
		}
		if !slices.IsSorted(enums[enum.Type]) || len(slices.Compact(slices.Clone(enums[enum.Type]))) != len(enums[enum.Type]) {
			t.Fatalf("%s values are unordered or duplicated", enum.Type)
		}
		for _, example := range reference.Enums[enum.Type].Examples {
			if !slices.Contains(enums[enum.Type], example) {
				t.Errorf("%s is missing native completion %s", enum.Type, example)
			}
		}
	}
	constraints := make(map[string]map[string][]byte)
	for _, constraint := range decoded.Constraints {
		if int(constraint.EnumIndex) >= len(decoded.Enums) || int(constraint.EnumValueIndex) >= len(decoded.EnumValues) {
			t.Fatalf("constraint references a missing enum or value: %+v", constraint)
		}
		name := decoded.Enums[constraint.EnumIndex].Type
		value := decoded.EnumValues[constraint.EnumValueIndex]
		if constraints[name] == nil {
			constraints[name] = make(map[string][]byte)
		}
		if _, exists := constraints[name][value]; exists {
			t.Fatalf("duplicate constraint for %s.%s", name, value)
		}
		constraints[name][value] = constraint.Constraints
	}
	for name, enum := range reference.Enums {
		for value, want := range enum.Constraints {
			if got := constraints[name][value]; !slices.Equal(got, want) {
				t.Errorf("%s.%s constraints = %v, BDS = %v", name, value, got, want)
			}
		}
	}
	// Check every registry entry, including metadata variants and block state
	// permutations, rather than only a hand-picked list of vanilla names.
	for _, item := range world.Items() {
		name, _ := item.EncodeItem()
		if !slices.Contains(enums["Item"], name) {
			t.Errorf("registered item %s is missing", name)
		}
	}
	for _, block := range world.DefaultBlockRegistry.Blocks() {
		name, _ := block.EncodeBlock()
		if !slices.Contains(enums["Block"], name) {
			t.Errorf("registered block %s is missing", name)
		}
	}
	again, _ := session.BuildAvailableCommands(commands, nil, make(map[string]struct{}))
	_, encodedAgain := decodeRegistryCommands(t, again)
	if !bytes.Equal(encoded, encodedAgain) {
		t.Fatal("unchanged registries produced a different command packet")
	}
}

func decodeRegistryCommands(t *testing.T, pk *mc.AvailableCommandsPacket) (mc.AvailableCommandsPacket, []byte) {
	t.Helper()
	encoded := encodePacket(pk)
	return decodeCommands(t, encoded), encoded
}
