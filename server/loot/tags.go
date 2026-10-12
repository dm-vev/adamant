package loot

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"

	"github.com/df-mc/dragonfly/server/item"
)

type itemTag struct {
	Values []json.RawMessage `json:"values"`
}

func loadItemTags() (map[string]map[string]struct{}, error) {
	return loadTags("item", nil, nil)
}

func loadEnchantmentTags() (map[string]map[string]struct{}, map[string]struct{}, error) {
	registered := make(map[string]struct{}, len(sourceEnchantmentDefinitions))
	for _, definition := range sourceEnchantmentDefinitions {
		registered[definition.Name] = struct{}{}
	}
	orderedEnchantmentTags = make(map[string][]string)
	tags, err := loadTags("enchantment", registered, orderedEnchantmentTags)
	if err != nil {
		return nil, nil, err
	}
	return tags, registered, nil
}

func loadTags(kind string, registered map[string]struct{}, ordered map[string][]string) (map[string]map[string]struct{}, error) {
	root := "assets/minecraft/tags/" + kind
	definitions := make(map[string][]string)
	err := fs.WalkDir(assets, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".json") {
			return err
		}
		b, err := assets.ReadFile(path)
		if err != nil {
			return err
		}
		var tag itemTag
		if err := json.Unmarshal(b, &tag); err != nil {
			return fmt.Errorf("decode %s tag %s: %w", kind, path, err)
		}
		id := "minecraft:" + strings.TrimSuffix(strings.TrimPrefix(path, root+"/"), ".json")
		for _, raw := range tag.Values {
			var value string
			if len(raw) > 0 && raw[0] == '"' {
				if err := json.Unmarshal(raw, &value); err != nil {
					return err
				}
			} else {
				var entry struct {
					ID string `json:"id"`
				}
				if err := json.Unmarshal(raw, &entry); err != nil {
					return err
				}
				value = entry.ID
			}
			if value == "" {
				return fmt.Errorf("%s tag %s has an empty value", kind, id)
			}
			definitions[id] = append(definitions[id], value)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	compiled := make(map[string]map[string]struct{}, len(definitions))
	visiting := make(map[string]bool)
	var resolve func(string) (map[string]struct{}, error)
	resolve = func(id string) (map[string]struct{}, error) {
		if tag, ok := compiled[id]; ok {
			return tag, nil
		}
		if visiting[id] {
			return nil, fmt.Errorf("cyclic builtin %s tag %s", kind, id)
		}
		values, ok := definitions[id]
		if !ok {
			return nil, fmt.Errorf("builtin %s tag references missing tag %s", kind, id)
		}
		visiting[id] = true
		members := make(map[string]struct{})
		if ordered != nil {
			ordered[id] = nil
		}
		add := func(value string) {
			if _, exists := members[value]; exists {
				return
			}
			members[value] = struct{}{}
			if ordered != nil {
				ordered[id] = append(ordered[id], value)
			}
		}
		for _, value := range values {
			if strings.HasPrefix(value, "#") {
				tag, err := resolve(normalize(value[1:]))
				if err != nil {
					return nil, err
				}
				if ordered == nil {
					for member := range tag {
						add(member)
					}
				} else {
					for _, member := range ordered[normalize(value[1:])] {
						add(member)
					}
				}
			} else {
				value = normalize(value)
				if registered != nil {
					if _, ok := registered[value]; !ok {
						continue
					}
				}
				add(value)
			}
		}
		delete(visiting, id)
		compiled[id] = members
		return members, nil
	}
	for id := range definitions {
		if _, err := resolve(id); err != nil {
			return nil, err
		}
	}
	return compiled, nil
}

func itemInTag(itemID, tagID string) bool {
	if itemTags == nil {
		return false
	}
	if !strings.Contains(tagID, ":") {
		tagID = "minecraft:" + tagID
	}
	members, ok := itemTags[tagID]
	if !ok {
		return false
	}
	_, ok = members[normalize(itemID)]
	return ok
}
func enchantmentID(enchantment item.EnchantmentType) string {
	return normalize("minecraft:" + strings.ToLower(strings.ReplaceAll(enchantment.Name(), " ", "_")))
}

func enchantmentTagMembers(tagID string) (map[string]struct{}, error) {
	if !strings.Contains(tagID, ":") {
		tagID = "minecraft:" + tagID
	}
	members, ok := enchantmentTags[tagID]
	if !ok {
		return nil, fmt.Errorf("unknown builtin enchantment tag %q", tagID)
	}
	return members, nil
}

func enchantmentKnown(id string) bool {
	_, ok := registeredEnchantments[normalize(id)]
	return ok
}
