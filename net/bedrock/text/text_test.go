package text

import "testing"

func TestColourf(t *testing.T) {
	for in, want := range map[string]string{
		"<red>Hello <bold>World</bold>!</red>": "§r§cHello §r§c§lWorld§r§c!§r",
		"plain":                                "§rplain",
		"<unknown>x</unknown>":                 "§r<unknown>x</unknown>",
		"a &amp; b <!-- note -->c":             "§ra & b c",
		"":                                     "",
	} {
		if got := Colourf("%s", in); got != want {
			t.Errorf("Colourf(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Colourf("<red>%v</red>", 3); got != "§r§c3§r" {
		t.Errorf("formatted %q", got)
	}
}

func TestClean(t *testing.T) {
	if got := Clean("§cRed §lbold§r §zkept"); got != "Red bold §zkept" {
		t.Fatalf("Clean = %q", got)
	}
}

func FuzzColourf(f *testing.F) {
	f.Add("<red>a</red><script>x</script>&amp;<!-- c --></")
	f.Fuzz(func(t *testing.T, s string) {
		_ = Colourf("%s", s)
		_ = Clean(s)
		_ = ANSI(s)
	})
}
