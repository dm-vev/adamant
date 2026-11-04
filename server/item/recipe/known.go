package recipe

import "sync"

// known holds the Java Edition IDs of the recipes registered, made of as
// many recipes as count says.
var known struct {
	sync.Mutex
	count int
	ids   map[string]struct{}
}

// KnownJavaID reports whether id is the Java Edition ID of a registered
// recipe: one JavaRecipeIDs returns for a recipe of Recipes(), of any kind,
// whether an edition shows recipes of the kind or not. The set is made on
// first use, and again once more recipes were registered.
func KnownJavaID(id string) bool {
	known.Lock()
	defer known.Unlock()
	if n := recipeCount(); known.ids == nil || known.count != n {
		all := Recipes()
		ids := make(map[string]struct{}, len(all))
		for _, r := range all {
			for _, javaID := range JavaRecipeIDs(r) {
				ids[javaID] = struct{}{}
			}
		}
		known.count, known.ids = len(all), ids
	}
	_, ok := known.ids[id]
	return ok
}
