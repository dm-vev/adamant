package entity

import (
	"slices"
)

// javaAttribute ports an AttributeInstance: a base value and the modifiers
// that change it, kept as Java saves them in attributes.
type mobAttribute struct {
	ID        string
	Base      float64
	Modifiers []mobAttributeModifier
}

// javaAttributeModifier ports an AttributeModifier.
type mobAttributeModifier struct {
	ID        string
	Amount    float64
	Operation string
}

// value ports AttributeInstance.calculateValue, clamped to the range of the
// attribute.
func (a *mobAttribute) value(minValue, maxValue float64) float64 {
	v := a.Base
	for _, m := range a.Modifiers {
		if m.Operation == "add_value" {
			v += m.Amount
		}
	}
	added := v
	for _, m := range a.Modifiers {
		if m.Operation == "add_multiplied_base" {
			v += added * m.Amount
		}
	}
	for _, m := range a.Modifiers {
		if m.Operation == "add_multiplied_total" {
			v *= 1 + m.Amount
		}
	}
	return max(minValue, min(maxValue, v))
}

// modifier returns the amount of the modifier with the id passed, if the
// attribute has one.
func (a *mobAttribute) modifier(id string) (float64, bool) {
	for _, m := range a.Modifiers {
		if m.ID == id {
			return m.Amount, true
		}
	}
	return 0, false
}

// setModifier ports AttributeInstance.addOrReplacePermanentModifier.
func (a *mobAttribute) setModifier(id string, amount float64, operation string) {
	for i := range a.Modifiers {
		if a.Modifiers[i].ID == id {
			a.Modifiers[i] = mobAttributeModifier{ID: id, Amount: amount, Operation: operation}
			return
		}
	}
	a.Modifiers = append(a.Modifiers, mobAttributeModifier{ID: id, Amount: amount, Operation: operation})
}

// javaAttributes holds the Java attributes of a mob by their ids.
type mobAttributes map[string]*mobAttribute

// get returns the attribute with the id passed, with the base def if the
// mob has none yet.
func (m *mobAttributes) get(id string, def float64) *mobAttribute {
	if *m == nil {
		*m = mobAttributes{}
	}
	if a, ok := (*m)[id]; ok {
		return a
	}
	a := &mobAttribute{ID: id, Base: def}
	(*m)[id] = a
	return a
}

// decodeJavaAttributes reads the attributes list Java saves.
func decodeMobAttributes(raw any) mobAttributes {
	list, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := mobAttributes{}
	for _, v := range list {
		entry, ok := v.(map[string]any)
		if !ok {
			continue
		}
		id, _ := entry["id"].(string)
		if id == "" {
			continue
		}
		a := &mobAttribute{ID: id, Base: oceanNumber(entry["base"])}
		if mods, ok := entry["modifiers"].([]any); ok {
			for _, mv := range mods {
				m, ok := mv.(map[string]any)
				if !ok {
					continue
				}
				mid, _ := m["id"].(string)
				op, _ := m["operation"].(string)
				a.Modifiers = append(a.Modifiers, mobAttributeModifier{ID: mid, Amount: oceanNumber(m["amount"]), Operation: op})
			}
		}
		out[id] = a
	}
	return out
}

// encode writes the attributes as Java saves them.
func (m mobAttributes) encode() []any {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	out := make([]any, 0, len(ids))
	for _, id := range ids {
		a := m[id]
		entry := map[string]any{"id": id, "base": a.Base}
		if len(a.Modifiers) != 0 {
			mods := make([]any, 0, len(a.Modifiers))
			for _, mod := range a.Modifiers {
				mods = append(mods, map[string]any{"id": mod.ID, "amount": mod.Amount, "operation": mod.Operation})
			}
			entry["modifiers"] = mods
		}
		out = append(out, entry)
	}
	return out
}
