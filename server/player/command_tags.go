package player

import "slices"

// CommandTags returns the Java command tags retained for the player.
func (p *Player) CommandTags() []string {
	tags, _ := p.javaState["Tags"].([]string)
	if tags == nil {
		if raw, ok := p.javaState["Tags"].([]any); ok {
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

// SetCommandTags replaces the Java command tags retained for the player.
func (p *Player) SetCommandTags(tags []string) {
	if p.javaState == nil {
		p.javaState = make(map[string]any)
	}
	p.javaState["Tags"] = slices.Clone(tags)
}

// SetCommandAttributeModifier stores a command modifier in persistent Java player state.
func (p *Player) SetCommandAttributeModifier(attribute, id string, amount float64, operation string, remove bool) bool {
	if p.javaState == nil {
		p.javaState = make(map[string]any)
	}
	all, _ := p.javaState["CommandAttributeModifiers"].(map[string]any)
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
	p.javaState["CommandAttributeModifiers"] = all
	return true
}

// CommandAttributeModifier reads a stored command modifier amount.
func (p *Player) CommandAttributeModifier(attribute, id string) (float64, bool) {
	all, _ := p.javaState["CommandAttributeModifiers"].(map[string]any)
	modifiers, _ := all[attribute].(map[string]any)
	raw, ok := modifiers[id].(map[string]any)
	if !ok {
		return 0, false
	}
	value, ok := raw["amount"].(float64)
	return value, ok
}
