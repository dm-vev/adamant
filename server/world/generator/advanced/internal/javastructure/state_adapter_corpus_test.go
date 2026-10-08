package javastructure

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/internal/javadata"
	"github.com/df-mc/dragonfly/server/world"
)

// TestStateAdapterCorpus covers embedded Java structure palettes, explicit
// feature/provider block states, and their builtin block-name references.
// Set JAVA26_BLOCK_STATE_CORPUS to the JSONL output of BlockStateCorpusOracle.java
// to include the complete running Java builtin block-state registry.
func TestStateAdapterCorpus(t *testing.T) {
	registry := world.DefaultBlockRegistry
	registry.Finalize()
	seen := map[string]struct{}{}
	reachableNative := map[uint32]struct{}{}
	reachableNames := map[string]struct{}{}
	failures := 0
	checked := 0
	check := func(source string, state world.BlockState) {
		key := fmt.Sprintf("%s|%v", state.Name, state.Properties)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		checked++
		placement := Block{State: state}
		actorBlock, actorBacked, err := NativeBlockForJavaState(state)
		if err != nil {
			t.Errorf("%s state %s: actor translation: %v", source, key, err)
			failures++
			return
		}
		if actorBacked {
			native, err := world.ResolveBlock(registry, actorBlock)
			if err != nil {
				t.Errorf("%s state %s: native actor state: %v", source, key, err)
				failures++
				return
			}
			base, ok := registry.BlockByRuntimeID(native.Block)
			if !ok {
				t.Fatalf("native actor runtime ID %d is unregistered", native.Block)
			}
			data := actorBlock.(world.NBTer).EncodeNBT()
			restored := base.(world.NBTer).DecodeNBT(data).(world.Block)
			if !reflect.DeepEqual(restored.(world.NBTer).EncodeNBT(), data) {
				t.Errorf("%s state %s: native actor persistence lost properties", source, key)
				failures++
			}
			if statue, ok := restored.(block.CopperGolemStatue); ok {
				name, _ := statue.EncodeBlock()
				if pose, exists := state.Properties["copper_golem_pose"]; exists && strings.ToLower(statue.Pose.Name()) != pose {
					t.Errorf("%s state %s: native actor lost source pose", source, key)
				}
				if facing, exists := state.Properties["facing"]; exists && statue.Facing.String() != facing || name != state.Name {
					t.Errorf("%s state %s: native actor lost source facing or identity", source, key)
				}
			}
			placement.NativeBlock = restored
		}
		native, err := placement.ResolveNative(registry)
		if err != nil {
			if failures < 40 {
				t.Errorf("%s state %s: ResolveState: %v", source, key, err)
			}
			failures++
			return
		}
		reachableNative[native.Block] = struct{}{}
		name, _, ok := registry.RuntimeIDToState(native.Block)
		if ok {
			reachableNames[name] = struct{}{}
		}
		java, err := world.JavaState(registry, native)
		if err != nil {
			if failures < 40 {
				t.Errorf("%s state %s: JavaState: %v", source, key, err)
			}
			failures++
			return
		}
		roundTrip := Block{State: java}
		roundTrip.NativeBlock, _, err = NativeBlockForJavaState(java)
		if err != nil {
			t.Fatalf("%s state %s: reverse actor state: %v", source, key, err)
		}
		again, err := roundTrip.ResolveNative(registry)
		if err != nil || again != native {
			if failures < 40 {
				t.Errorf("%s state %s: round trip produced %#v, want %#v (error %v)", source, key, again, native, err)
			}
			failures++
		}
	}

	templateCount := 0
	if err := fs.WalkDir(javadata.Assets(), "assets/data/minecraft/structure", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(name, ".nbt") {
			return nil
		}
		templateName := strings.TrimSuffix(strings.TrimPrefix(name, "assets/data/minecraft/structure/"), ".nbt")
		template, err := Load(templateName)
		if err != nil {
			return err
		}
		templateCount++
		for paletteIndex, palette := range template.Palettes {
			for _, block := range palette {
				check(fmt.Sprintf("template %s palette %d", templateName, paletteIndex), block.State)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("walking Java templates: %v", err)
	}
	if templateCount == 0 {
		t.Fatal("embedded Java template corpus is empty")
	}

	featureCount := 0
	for _, resource := range []struct{ kind, directory string }{
		{"configured_feature", "assets/data/minecraft/worldgen/feature"},
		{"placed_feature", "assets/data/minecraft/worldgen/placed_feature"},
		{"block_state_provider", "assets/data/minecraft/worldgen/block_state_provider"},
	} {
		if err := fs.WalkDir(javadata.Assets(), resource.directory, func(file string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(file, ".json") {
				return nil
			}
			name := strings.TrimSuffix(strings.TrimPrefix(file, resource.directory+"/"), ".json")
			config, err := javadata.LoadJSON(resource.kind, name)
			if err != nil {
				return err
			}
			featureCount++
			walkBlockStates(config, func(state world.BlockState) { check(resource.kind+" "+name, state) })
			return nil
		}); err != nil {
			t.Fatalf("walking %s assets: %v", resource.kind, err)
		}
	}
	if featureCount == 0 {
		t.Fatal("embedded Java feature corpus is empty")
	}

	corpusPath := os.Getenv("JAVA26_BLOCK_STATE_CORPUS")
	if corpusPath != "" {
		file, err := os.Open(corpusPath)
		if err != nil {
			t.Fatalf("opening Java builtin state corpus: %v", err)
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 4096), 1<<20)
		line := 0
		for scanner.Scan() {
			line++
			var record struct {
				Name       string
				Properties map[string]any
			}
			if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
				t.Errorf("Java builtin corpus line %d: %v", line, err)
				failures++
				continue
			}
			check("Java builtin line "+fmt.Sprint(line), world.BlockState{Name: record.Name, Properties: world.DecodeStateProperties(record.Properties)})
		}
		if err := scanner.Err(); err != nil {
			t.Errorf("reading Java builtin corpus: %v", err)
			failures++
		}
		if err := file.Close(); err != nil {
			t.Errorf("closing Java builtin corpus: %v", err)
			failures++
		}
		if line == 0 {
			t.Error("Java builtin state corpus is empty")
		}
	}

	// Reverse-check each native state family reached by a Java builtin/template/config
	// state, including Bedrock property variants not selected by the Java forward map.
	reverseFailures := 0
	for rid := range uint32(registry.BlockCount()) {
		name, properties, ok := registry.RuntimeIDToState(rid)
		if !ok {
			t.Errorf("native registry omitted runtime ID %d", rid)
			reverseFailures++
			continue
		}
		if _, relevant := reachableNames[name]; !relevant {
			continue
		}
		if name == "minecraft:leaf_litter" && properties["growth"].(int32) > 3 {
			if _, reached := reachableNative[rid]; reached {
				t.Errorf("Java forward mapping selected native-only leaf litter count %v", properties)
			}
			if _, err := world.JavaState(registry, world.NativeState{Block: rid, Liquid: registry.AirRuntimeID()}); err == nil {
				t.Errorf("native leaf litter exceeding four segments was accepted as a Java state: %v", properties)
			}
			continue
		}
		if (strings.HasSuffix(name, "_head") || strings.HasSuffix(name, "_skull")) && properties["facing_direction"] == int32(0) {
			if _, reached := reachableNative[rid]; reached {
				t.Errorf("Java forward mapping selected nonrepresentable ceiling skull %d", rid)
			}
			if _, err := world.JavaState(registry, world.NativeState{Block: rid, Liquid: registry.AirRuntimeID()}); err == nil {
				t.Errorf("native ceiling skull was accepted as a Java state: %s", name)
			}
			continue
		}
		if properties["torch_facing_direction"] == "unknown" {
			switch name {
			case "minecraft:torch", "minecraft:soul_torch", "minecraft:redstone_torch", "minecraft:unlit_redstone_torch":
				if _, reached := reachableNative[rid]; reached {
					t.Errorf("Java forward mapping selected nonrepresentable native torch state %d", rid)
				}
				if _, err := world.JavaState(registry, world.NativeState{Block: rid, Liquid: registry.AirRuntimeID()}); err == nil {
					t.Errorf("native unknown torch direction was accepted as a Java state: %s", name)
				}
				continue
			}
		}
		if name == "minecraft:jigsaw" && properties["facing_direction"].(int32) >= 2 && properties["rotation"].(int32) != 0 {
			if _, reached := reachableNative[rid]; reached {
				t.Errorf("Java forward mapping selected nonrepresentable native jigsaw state %d", rid)
			}
			if _, err := world.JavaState(registry, world.NativeState{Block: rid, Liquid: registry.AirRuntimeID()}); err == nil {
				t.Errorf("native horizontal jigsaw with nonzero rotation was accepted as a Java state: %v", properties)
			}
			continue
		}
		if name == "minecraft:structure_block" && (properties["structure_block_type"] == "export" || properties["structure_block_type"] == "invalid") {
			if _, reached := reachableNative[rid]; reached {
				t.Errorf("Java forward mapping selected native-only structure mode %v", properties)
			}
			if _, err := world.JavaState(registry, world.NativeState{Block: rid, Liquid: registry.AirRuntimeID()}); err == nil {
				t.Errorf("native-only structure mode was accepted as a Java state: %v", properties)
			}
			continue
		}
		if _, err := world.JavaState(registry, world.NativeState{Block: rid, Liquid: registry.AirRuntimeID()}); err != nil {
			if reverseFailures < 40 {
				t.Errorf("reverse native state %s (RID %d): %v", name, rid, err)
			}
			reverseFailures++
		}
	}
	t.Logf("checked %d unique Java block states from %d templates and %d feature/provider configs; %d native primary states reached; %d reverse states failed", checked, templateCount, featureCount, len(reachableNative), reverseFailures)
	if failures > 40 {
		t.Errorf("%d additional forward/round-trip codec failures omitted", failures-40)
	}
	if reverseFailures > 40 {
		t.Errorf("%d additional reverse-state failures omitted", reverseFailures-40)
	}
}

func walkBlockStates(value any, visit func(world.BlockState)) {
	switch value := value.(type) {
	case map[string]any:
		name, hasName := value["Name"].(string)
		if !hasName {
			name, hasName = value["name"].(string)
		}
		if !hasName {
			name, hasName = value["id"].(string)
		}
		properties, hasProperties := value["Properties"].(map[string]any)
		if !hasProperties {
			properties, hasProperties = value["properties"].(map[string]any)
		}
		if hasName && (hasProperties || value["Name"] != nil || len(value) == 1) {
			visit(world.BlockState{Name: name, Properties: world.DecodeStateProperties(properties)})
		}
		for key, child := range value {
			// Spring configurations use a FluidState codec, not a BlockState codec.
			if key == "state" && value["type"] == "minecraft:spring_feature" {
				continue
			}
			// Template identifiers belong to the structure registry, not the block registry.
			if key == "templates" && value["type"] == "minecraft:template" {
				continue
			}
			walkBlockStates(child, visit)
		}
	case []any:
		for _, child := range value {
			walkBlockStates(child, visit)
		}
	case string:
		// The executed Java registry distinguishes builtin block IDs from other
		// resource references; bare block-state strings select default states.
		if _, err := javadata.BlockIsSolid(value); err == nil {
			if !strings.Contains(value, ":") {
				value = "minecraft:" + value
			}
			visit(world.BlockState{Name: value})
		}
	}
}
