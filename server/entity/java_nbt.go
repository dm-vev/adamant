package entity

import (
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

type javaNBTConfig struct {
	typeOf world.EntityType
	data   map[string]any
	native map[string]any
}

func (c javaNBTConfig) Apply(data *world.EntityData) {
	source := world.CloneJavaEntityNBT(c.data)
	if c.native != nil {
		data.NativeNBT = c.native
		data.NativeNBT["adamant:java"] = source
		decoded := make(map[string]any, len(source)+len(c.native))
		for key, value := range source {
			decoded[key] = value
		}
		for key, value := range c.native {
			decoded[key] = value
		}
		c.typeOf.DecodeNBT(decoded, data)
		return
	}
	switch c.typeOf.EncodeEntity() {
	case "minecraft:cat", "minecraft:witch":
		// Their codecs use the Java namespace for source-only spawn state.
		// Do not mislabel Java attributes or sound variants as BDS actor fields.
		data.NativeNBT = map[string]any{"adamant:java": source}
	default:
		data.NativeNBT = source
	}
	c.typeOf.DecodeNBT(c.data, data)
	decodePersistence(data, c.data)
}

// NewFromStructureNBT constructs a worldless entity handle from Java entity NBT.
// opts supplies the absolute, already-transformed position and rotation; all
// type-specific fields are decoded by the registered entity type.
func NewFromStructureNBT(data map[string]any, opts world.EntitySpawnOpts) (*world.EntityHandle, error) {
	id, ok := data["id"].(string)
	if !ok || id == "" {
		return nil, fmt.Errorf("Java entity NBT has no string id")
	}
	if len(id) > len("minecraft:") && !strings.HasPrefix(id, "minecraft:") {
		return nil, fmt.Errorf("unsupported Java entity namespace in %q", id)
	}
	if err := validateJavaEntityEquipment(id, data); err != nil {
		return nil, err
	}
	t, ok := DefaultRegistry.Lookup(id)
	if id == "minecraft:end_crystal" {
		t, ok = EndCrystalType, true
	}
	if !ok {
		return nil, fmt.Errorf("unsupported Java entity type %q", id)
	}
	native, err := projectTemplateMobNBT(data)
	if err != nil {
		return nil, err
	}
	if native != nil {
		if err := applyTrialEquipment(opts, native, id); err != nil {
			return nil, err
		}
	}
	return opts.New(t, javaNBTConfig{typeOf: t, data: data, native: native}), nil
}

// NewFromBucket constructs a mob from the entity data retained by its bucket.
// The bucket flag is set before the registered type decodes its NBT.
func NewFromBucket(opts world.EntitySpawnOpts, entityID string, data map[string]any) *world.EntityHandle {
	nbt := make(map[string]any, len(data)+2)
	for key, value := range data {
		nbt[key] = value
	}
	nbt["id"] = entityID
	nbt["FromBucket"] = true
	handle, err := NewFromStructureNBT(nbt, opts)
	if err != nil {
		return nil
	}
	return handle
}
func validateJavaEntityEquipment(id string, data map[string]any) error {
	if id != "minecraft:wolf" {
		return nil
	}
	equipment, ok := data["equipment"].(map[string]any)
	if !ok {
		return nil
	}
	body, ok := equipment["body"].(map[string]any)
	if !ok {
		body, ok = equipment["minecraft:body"].(map[string]any)
	}
	if !ok {
		return nil
	}
	stack, err := item.StackFromStructureNBT(body)
	if err != nil {
		return fmt.Errorf("invalid wolf body armor: %w", err)
	}
	if _, ok := stack.Item().(item.WolfArmor); !ok && !stack.Empty() {
		return fmt.Errorf("wolf body equipment is %T, want wolf armor", stack.Item())
	}
	return nil
}
