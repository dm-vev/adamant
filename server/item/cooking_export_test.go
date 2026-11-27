package item

// JavaCookingItemIDs returns every Java item the generated cooking recipes
// and fuels name.
func CookingItemIDs() (ids []string) {
	for _, r := range cookingRecipesList {
		ids = append(ids, r.ingredient, r.result)
	}
	for id := range cookingFuelItem {
		ids = append(ids, id)
	}
	return ids
}
