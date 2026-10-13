package entity

import "slices"

// CommandTags returns the Java command tags retained for the entity.
func (e *Ent) CommandTags() []string {
	state := ReadNativeMobJavaState(e.data.NativeNBT)
	tags, _ := state["Tags"].([]string)
	if tags == nil {
		if raw, ok := state["Tags"].([]any); ok {
			tags = make([]string, 0, len(raw))
			for _, value := range raw {
				if tag, ok := value.(string); ok {
					tags = append(tags, tag)
				}
			}
		}
	}
	return slices.Clone(tags)
}

// SetCommandTags replaces the Java command tags retained for the entity.
func (e *Ent) SetCommandTags(tags []string) {
	state := ReadNativeMobJavaState(e.data.NativeNBT)
	if state == nil {
		state = make(map[string]any)
	}
	state["Tags"] = slices.Clone(tags)
	e.data.NativeNBT = MergeNativeMobJavaState(e.data.NativeNBT, state)
}

// NativeAttribute returns the base value of a retained native mob attribute.
func (e *Ent) NativeAttribute(name string) (float64, bool) {
	attribute, ok := nativeMobAttribute(e.data.NativeNBT, name)
	return attribute.Base, ok
}

// NativeAttributeValue returns the current value of a retained native mob attribute.
func (e *Ent) NativeAttributeValue(name string) (float64, bool) {
	attribute, ok := nativeMobAttribute(e.data.NativeNBT, name)
	return attribute.Current, ok
}

// SetNativeAttribute changes the base value of a retained native mob attribute.
func (e *Ent) SetNativeAttribute(name string, value float64) bool {
	attributes := ReadNativeMobAttributes(e.data.NativeNBT)
	for i := range attributes {
		if attributes[i].Name == name {
			attributes[i].Base, attributes[i].Current = value, value
			e.data.NativeNBT = MergeNativeMobNBT(e.data.NativeNBT, map[string]any{"Attributes": EncodeNativeMobAttributes(attributes, nil)})
			return true
		}
	}
	return false
}

// SetCommandAttributeModifier stores a Java command modifier alongside source-only state.
func (e *Ent) SetCommandAttributeModifier(attribute, id string, amount float64, operation string, remove bool) bool {
	state := ReadNativeMobJavaState(e.data.NativeNBT)
	if state == nil {
		state = make(map[string]any)
	}
	all, _ := state["CommandAttributeModifiers"].(map[string]any)
	if all == nil {
		all = make(map[string]any)
	}
	modifiers, _ := all[attribute].(map[string]any)
	if modifiers == nil {
		modifiers = make(map[string]any)
	}
	if remove {
		if _, ok := modifiers[id]; !ok {
			return false
		}
		delete(modifiers, id)
	} else {
		modifiers[id] = map[string]any{"amount": amount, "operation": operation}
	}
	if len(modifiers) == 0 {
		delete(all, attribute)
	} else {
		all[attribute] = modifiers
	}
	state["CommandAttributeModifiers"] = all
	e.data.NativeNBT = MergeNativeMobJavaState(e.data.NativeNBT, state)
	return true
}

// CommandAttributeModifier reads the amount stored for a Java command modifier.
func (e *Ent) CommandAttributeModifier(attribute, id string) (float64, bool) {
	state := ReadNativeMobJavaState(e.data.NativeNBT)
	all, _ := state["CommandAttributeModifiers"].(map[string]any)
	modifiers, _ := all[attribute].(map[string]any)
	raw, ok := modifiers[id].(map[string]any)
	if !ok {
		return 0, false
	}
	value, ok := raw["amount"].(float64)
	return value, ok
}
