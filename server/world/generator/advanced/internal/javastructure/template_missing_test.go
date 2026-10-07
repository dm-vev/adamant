package javastructure

import "testing"

func TestLoadOrCreateMissingPoolTemplateUsesJavaBlankTemplate(t *testing.T) {
	const name = "ancient_city/walls/intact_horizontal_wall_stairs_5"
	template, err := LoadOrCreate(name)
	if err != nil {
		t.Fatalf("LoadOrCreate(%q): %v", name, err)
	}
	if template.Size != [3]int32{} || len(template.Palettes) != 0 || len(template.Entities) != 0 {
		t.Fatalf("missing template = size %v, %d palettes, %d entities; want Java blank template", template.Size, len(template.Palettes), len(template.Entities))
	}
	if _, err := Load(name); err == nil {
		t.Fatalf("Load(%q) unexpectedly accepted absent embedded resource", name)
	}
	placement, err := template.PlaceBlocks(0, [3]int32{}, None, NoMirror, Bounds{}, 0)
	if err != nil {
		t.Fatalf("placing Java blank template: %v", err)
	}
	if len(placement.Blocks) != 0 || len(placement.Entities) != 0 {
		t.Fatalf("blank template placement = %+v; want no blocks or entities", placement)
	}
}

func TestZeroSizeTemplateBoundsMatchJavaBoundingBox(t *testing.T) {
	template := &Template{}
	origin := [3]int32{10, 20, 30}
	if got, want := templateBounds(template, origin, None), (Box{Min: [3]int32{9, 19, 29}, Max: origin}); got != want {
		t.Fatalf("zero-size bounds = %+v, want %+v", got, want)
	}
}
