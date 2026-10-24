package builtin

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
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
	Register(registerTestAdapter{})
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
					if parameter.Type&(protocol.CommandArgValid|protocol.CommandArgEnum|protocol.CommandArgSoftEnum) != protocol.CommandArgValid|protocol.CommandArgEnum {
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

func TestBlockCompletionsUseSourceRegistry(t *testing.T) {
	registry := world.NewBlockRegistry()
	for _, variant := range []int32{0, 1} {
		registry.RegisterBlockState(world.BlockState{Name: "example:completion_block", Properties: map[string]any{"variant": variant}})
	}
	w := world.Config{Blocks: registry, Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		check := func(src cmd.Source) {
			t.Helper()
			values := (blockArgument("")).Options(src)
			if count := len(slices.DeleteFunc(slices.Clone(values), func(value string) bool { return value != "example:completion_block" })); count != 1 {
				t.Fatalf("custom block completion appears %d times, want once", count)
			}
			if slices.Contains(values, "completion_block") {
				t.Fatal("non-minecraft namespace was dropped from completion")
			}
		}
		check(registryParameterSource{tx: tx})
		check(cmd.WithContext(registryParameterSource{tx: tx}, cmd.ExecutionContext{}))
		check(cmd.WithContext(consoleTestSource{}, cmd.ExecutionContext{World: w}))
		if slices.Contains((blockArgument("")).Options(nil), "example:completion_block") {
			t.Fatal("custom block leaked into another registry's completions")
		}
	})
}

func TestItemCompletionsKeepCommandExecution(t *testing.T) {
	w := world.Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	<-w.Exec(func(tx *world.Tx) {
		p := tx.AddEntity(world.EntitySpawnOpts{}.New(player.Type, player.Config{Name: "RegistryTarget", GameMode: world.GameModeCreative})).(*player.Player)
		if out := runTestCommand(newGiveCommand(), "@a minecraft:diamond 2", tx); out.ErrorCount() != 0 {
			t.Fatalf("give errors: %v", out.Errors())
		}
		if got, _ := p.Inventory().Item(0); got.Count() != 2 || !got.Comparable(item.NewStack(item.Diamond{}, 1)) {
			t.Fatalf("give inventory = %v", got)
		}
		if out := runTestCommand(newItemCommand(), "@a 0 diamond 3", tx); out.ErrorCount() != 0 {
			t.Fatalf("item errors: %v", out.Errors())
		}
		if got, _ := p.Inventory().Item(0); got.Count() != 3 {
			t.Fatalf("item count = %d, want 3", got.Count())
		}
		for _, javaAlias := range []string{"minecraft:oak_button", "red_bed"} {
			if out := runTestCommand(newGiveCommand(), "@a "+javaAlias, tx); out.ErrorCount() != 0 {
				t.Errorf("give Java alias %s errors: %v", javaAlias, out.Errors())
			}
		}
	})
}

type registryParameterSource struct {
	cmd.Source
	tx *world.Tx
}

func (s registryParameterSource) Tx() *world.Tx { return s.tx }

func decodeRegistryCommands(t *testing.T, pk *packet.AvailableCommands) (packet.AvailableCommands, []byte) {
	t.Helper()
	var wire bytes.Buffer
	pk.Marshal(protocol.NewWriter(&wire, 0))
	encoded := bytes.Clone(wire.Bytes())
	var decoded packet.AvailableCommands
	decoded.Marshal(protocol.NewReader(&wire, 0, false))
	if wire.Len() != 0 {
		t.Fatalf("packet has %d unread bytes", wire.Len())
	}
	return decoded, encoded
}
