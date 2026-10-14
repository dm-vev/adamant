package world

import (
	"errors"
	"strings"
)

// structureTemplateProvider is a Provider that persists structure templates,
// as Bedrock keeps them in LevelDB under structuretemplate_ keys.
type structureTemplateProvider interface {
	// LoadStructureTemplate returns the .mcstructure NBT of the template saved
	// under the name, or false if there is none.
	LoadStructureTemplate(name string) (map[string]any, bool, error)
	// SaveStructureTemplate saves the .mcstructure NBT of a template under the
	// name.
	SaveStructureTemplate(name string, data map[string]any) error
}

// builtinStructureTemplates returns the templates every world ships with.
var builtinStructureTemplates func(w *World, name string) (*StructureTemplate, bool, error)

// SetBuiltinStructureTemplates sets the source of the structure templates
// every world ships with, like the templates of the vanilla data pack.
// Templates saved in a world take precedence over them.
func SetBuiltinStructureTemplates(f func(w *World, name string) (*StructureTemplate, bool, error)) {
	builtinStructureTemplates = f
}

// StructureTemplate looks up a structure template by name, ported from
// StructureTemplateManager.get: templates saved or loaded before, then the
// ones in the world save, then the built-in ones.
func (w *World) StructureTemplate(name string) (*StructureTemplate, bool, error) {
	if name == "" {
		return nil, false, nil
	}
	ref := w.providerUse
	ref.structureMu.Lock()
	t, ok := ref.structures[name]
	ref.structureMu.Unlock()
	if ok {
		return t, true, nil
	}
	if store, ok := w.conf.Provider.(structureTemplateProvider); ok {
		data, found, err := store.LoadStructureTemplate(name)
		if err != nil {
			return nil, false, err
		}
		if found {
			if t, err = DecodeStructureTemplate(data, w.conf.Blocks); err != nil {
				return nil, false, err
			}
			w.cacheStructureTemplate(name, t)
			return t, true, nil
		}
	}
	if builtinStructureTemplates == nil {
		return nil, false, nil
	}
	t, found, err := builtinStructureTemplates(w, name)
	if err != nil || !found {
		return nil, false, err
	}
	w.cacheStructureTemplate(name, t)
	return t, true, nil
}

// SaveStructureTemplate stores a template under a name, and in the world save
// if disk is true. A template only kept in memory is lost when the server
// stops or it is unloaded.
func (w *World) SaveStructureTemplate(name string, t *StructureTemplate, disk bool) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("structure template: empty name")
	}
	w.cacheStructureTemplate(name, t)
	if !disk {
		return nil
	}
	store, ok := w.conf.Provider.(structureTemplateProvider)
	if !ok {
		return errors.New("structure template: world provider cannot save structures")
	}
	return store.SaveStructureTemplate(name, t.EncodeNBT())
}

// UnloadStructureTemplate forgets a template kept in memory, as
// StructureTemplateManager.remove. A copy in the world save is loaded again
// the next time the template is looked up.
func (w *World) UnloadStructureTemplate(name string) {
	ref := w.providerUse
	ref.structureMu.Lock()
	delete(ref.structures, name)
	ref.structureMu.Unlock()
}

func (w *World) cacheStructureTemplate(name string, t *StructureTemplate) {
	ref := w.providerUse
	ref.structureMu.Lock()
	if ref.structures == nil {
		ref.structures = map[string]*StructureTemplate{}
	}
	ref.structures[name] = t
	ref.structureMu.Unlock()
}
