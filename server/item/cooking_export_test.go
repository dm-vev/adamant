package item

// JavaCookingItemIDs returns every Java item the generated cooking recipes
// and fuels name.
func JavaCookingItemIDs() (ids []string) {
	for _, r := range javaCookingRecipes {
		ids = append(ids, r.ingredient, r.result)
	}
	for id := range javaCookingFuel {
		ids = append(ids, id)
	}
	return ids
}
