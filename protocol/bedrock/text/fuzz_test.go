package bedrocktext

import (
	"testing"
)

// FuzzColourf compares Colourf with Colourf of net/bedrock/text: the text
// it returns renders as the string the old one gives, for the format read
// as markup, formatted with arguments, and passed as an argument.
func FuzzColourf(f *testing.F) {
	for in, want := range colourfCases {
		f.Add(in, want, int64(0))
	}
	for _, s := range netCorpus(f) {
		f.Add(s, s, int64(1))
	}
	for _, s := range fragments {
		f.Add(s, "", int64(-1))
		f.Add("<red>"+s+"%v</red>", s, int64(2))
	}
	f.Add("<red>%v</red>", "<b>x</b>", int64(3))
	f.Add("%[2]v<i>%[1]s</i>%d%%%q", "&lt;", int64(42))
	f.Add("<script>%s</script><title>%s&amp;</title>", "</script><red>x", int64(7))
	f.Add("<red><blue><red>x</red>y</blue>", "</red>", int64(0))
	f.Add("<RED class='a' b=\"c\" d=e/>%s<!-- %d --></", "<plaintext>", int64(0))
	f.Fuzz(func(t *testing.T, format, arg string, n int64) {
		checkColourf(t, format, nil)
		checkColourf(t, "%s", []any{format})
		checkColourf(t, format, []any{arg, n})
	})
}

// FuzzLegacy checks that ParseLegacy reads every string so that it renders
// as the string, has its plain text, and shows on the console as it.
func FuzzLegacy(f *testing.F) {
	for in, want := range colourfCases {
		f.Add(in)
		f.Add(want)
	}
	for _, s := range legacyFragments {
		f.Add(s)
		f.Add("a" + s + "§c" + s + "§r")
	}
	f.Add("§r§cHello §r§c§lWorld§r§c!§r")
	f.Add("§cRed §lbold§r §zkept")
	f.Add("§§c§r§r§k\xc2§\xa7§")
	f.Fuzz(func(t *testing.T, s string) {
		checkLegacy(t, s)
	})
}

// FuzzTree builds texts from random bytes and checks that each renders as
// it means, reads back as it renders, and shows on the console as its
// string does. With bedrock set, the texts hold only what Bedrock Edition
// shows, so that the text read back is equivalent to the text.
func FuzzTree(f *testing.F) {
	f.Add([]byte{}, false)
	f.Add([]byte{0, 0, 0, 1, 0, 200, 2, 0, 3}, true)
	f.Add([]byte{3, 0, 120, 6, 2, 3, 0, 1, 0, 8, 150, 2, 1, 2, 0, 2, 0, 2, 1}, true)
	f.Add([]byte{2, 2, 1, 0, 3, 0, 240, 1, 2, 3, 4, 1}, false)
	f.Add([]byte("random bytes of a text, long enough for some nesting and styles to come out of them"), true)
	f.Add([]byte("random bytes of a text, long enough for some nesting and styles to come out of them"), false)
	f.Fuzz(func(t *testing.T, data []byte, bedrock bool) {
		r := &treeReader{data: data, bedrock: bedrock}
		checkTree(t, r.text(5), bedrock)
	})
}
