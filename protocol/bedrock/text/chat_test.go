package bedrocktext

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/text"
	"golang.org/x/text/language"
)

// The tests of this file hold the translations of the chat package, built
// on structured text, to those it had before (chatoracle_test.go): for every
// language, Message of the text of a translation filled in is the message
// and the parameters Bedrock Edition clients were sent for it, and its
// fallback is the string the console and other subscribers that do not
// translate were shown.
//
// What is left out, by design: the parameters of a translation whose
// TranslationString resolves to a string without a % (no key, but text of
// the server's own; its arguments are not shown), as no such translation
// with arguments is sent to clients; a Translation that takes arguments
// passed as an argument, for which the old code panicked; and fallbacks
// with markup, references or % other than %v and %%, and Enc formats with
// other than one %v, none of which the repository writes.

// key is a TranslationString that resolves to itself, as the keys of the
// game do.
type key string

func (k key) Resolve(language.Tag) string { return string(k) }

// chatLocales are the languages the translations are checked in.
var chatLocales = []language.Tag{language.English, language.Russian}

// checkChat fails t if the translation nw filled in with the arguments
// newArgs does not give, for every language, the message, parameters and
// fallback that old, the same translation in the old code, gives filled in
// with oldArgs.
func checkChat(t testing.TB, name string, nw chat.Translation, old oldTranslation, newArgs, oldArgs []any) {
	t.Helper()
	nf, of := nw.F(newArgs...), old.F(oldArgs...)
	for _, l := range chatLocales {
		msg, params, translated := Message(nf.Text(l))
		if want := of.Resolve(l); msg != want {
			t.Fatalf("%s, %v: message %q, the old code sent %q\ntext %#v", name, l, msg, want, nf.Text(l))
		}
		if strings.HasPrefix(old.str.Resolve(l), "%") {
			if want := of.Params(l); !translated || !slices.Equal(params, want) {
				t.Fatalf("%s, %v: parameters %q (translated %v), the old code sent %q", name, l, params, translated, want)
			}
		} else if translated || params != nil {
			// No key: the text of the server shown as it is, without the
			// arguments.
			t.Fatalf("%s, %v: a translation without a key has parameters %q (translated %v)", name, l, params, translated)
		}
	}
	if got, want := nf.String(), of.String(); got != want {
		t.Fatalf("%s: fallback %q, the old code showed %q", name, got, want)
	}
	if got, want := nf.Error(), of.Error(); got != want {
		t.Fatalf("%s: error %q, the old code gave %q", name, got, want)
	}
}

// chatArg is an argument of a translation, in the new code and in the old.
type chatArg struct {
	name     string
	new, old any
}

// chatArgs returns arguments of every kind a translation is passed, with s
// as the string of those that take one.
func chatArgs(s string, n int) []chatArg {
	disconnect := oldRepoTranslations["chat.MessageServerDisconnect"]
	join := oldRepoTranslations["chat.MessageJoin"]
	about, oldAbout := descriptionTranslations("about")
	gamemode, oldGamemode := descriptionTranslations("gamemode")
	return []chatArg{
		{"a string", s, s},
		{"a string with codes", "§c" + s + "§r", "§c" + s + "§r"},
		{"a number", n, n},
		{"nil", nil, nil},
		{"a Translation", chat.MessageServerDisconnect, disconnect},
		{"a Translation of server text", about, oldAbout},
		{"a Translation of a description key", gamemode, oldGamemode},
		{"a translation filled in", chat.MessageServerDisconnect.F(), disconnect.F()},
		{"a translation filled in with arguments", chat.MessageJoin.F(s), join.F(s)},
		{"a translation of server text filled in", about.F(), oldAbout.F()},
		{"a TranslationString", key("%tile.stone.name"), key("%tile.stone.name")},
		{"a TranslationString of server text", key(s), key(s)},
		{"a mob", text.Translate("entity.cave_spider.name", "Cave Spider"), oldMobName("cave_spider")},
	}
}

// newRepoTranslations are the translations of the repository, by the names
// of oldRepoTranslations. The death messages, which the player package
// keeps to itself, are made from their source, as it makes them.
func newRepoTranslations(t testing.TB) map[string]chat.Translation {
	m := map[string]chat.Translation{
		"chat.MessageJoin":                  chat.MessageJoin,
		"chat.MessageQuit":                  chat.MessageQuit,
		"chat.MessageServerDisconnect":      chat.MessageServerDisconnect,
		"chat.MessageBedTooFar":             chat.MessageBedTooFar,
		"chat.MessageBedObstructed":         chat.MessageBedObstructed,
		"chat.MessageRespawnPointSet":       chat.MessageRespawnPointSet,
		"chat.MessageNoSleep":               chat.MessageNoSleep,
		"chat.MessageNoSleepMonsters":       chat.MessageNoSleepMonsters,
		"chat.MessageBedIsOccupied":         chat.MessageBedIsOccupied,
		"chat.MessageSleeping":              chat.MessageSleeping,
		"chat.MessageRespawnAnchorNotValid": chat.MessageRespawnAnchorNotValid,
		"chat.MessageBedNotValid":           chat.MessageBedNotValid,
		"chat.MessageAdminCommand":          chat.MessageAdminCommand,
		"cmd.MessageSyntax":                 cmd.MessageSyntax,
		"cmd.MessageUsage":                  cmd.MessageUsage,
		"cmd.MessageUnknown":                cmd.MessageUnknown,
		"cmd.MessageNoTargets":              cmd.MessageNoTargets,
		"cmd.MessageNumberInvalid":          cmd.MessageNumberInvalid,
		"cmd.MessageBooleanInvalid":         cmd.MessageBooleanInvalid,
		"cmd.MessagePlayerNotFound":         cmd.MessagePlayerNotFound,
		"cmd.MessageParameterInvalid":       cmd.MessageParameterInvalid,
	}
	// Every translation of the chat and cmd packages is in the table.
	for _, path := range []string{"server/player/chat/translate.go", "server/cmd/output.go"} {
		pkg := filepath.Base(filepath.Dir(path))
		for _, name := range sourceMatches(t, path, `(?m)^var (\w+) = (?:chat\.)?Translate\(`) {
			if _, ok := m[pkg+"."+name[1]]; !ok {
				t.Fatalf("%s.%s is not in the table", pkg, name[1])
			}
		}
	}
	// deathTranslation of the player package.
	deaths := sourceMatches(t, "server/player/death_message.go", `deathTranslation\("([^"]+)", (\d+), ("(?:[^"\\]|\\.)*")\)`)
	if len(deaths) < 30 {
		t.Fatalf("found %d death messages", len(deaths))
	}
	for _, d := range deaths {
		params, _ := strconv.Atoi(d[2])
		fallback, err := strconv.Unquote(d[3])
		if err != nil {
			t.Fatal(err)
		}
		m["player."+d[1]] = chat.Translate(key("%"+d[1]), params, fallback)
	}
	return m
}

// sourceMatches returns the submatches of the expression passed in the
// source file of the repository at path.
func sourceMatches(t testing.TB, path, expr string) [][]string {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", filepath.FromSlash(path)))
	if err != nil {
		t.Fatal(err)
	}
	return regexp.MustCompile(expr).FindAllStringSubmatch(string(b), -1)
}

// commandDescription is the TranslationString of the description of a
// builtin command, as newCommand of server/cmd/builtin makes it: the key of
// a description of the client, or the text of the catalog of the server.
type commandDescription struct{ key, nativeKey string }

func (d commandDescription) Resolve(l language.Tag) string {
	if d.nativeKey != "" {
		return "%" + d.nativeKey
	}
	if l == language.Russian {
		if s, ok := descriptionCatalog("ru")[d.key]; ok {
			return s
		}
	}
	return descriptionCatalog("en")[d.key]
}

// descriptionCatalog returns the descriptions of builtin commands of the
// language passed.
func descriptionCatalog(lang string) map[string]string {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "server", "cmd", "builtin", "locales", lang+".json"))
	if err != nil {
		panic(err)
	}
	var m map[string]string
	if err := json.Unmarshal(b, &m); err != nil {
		panic(err)
	}
	return m
}

// nativeDescriptions returns the keys of the descriptions of the client
// that builtin commands use, by command.
func nativeDescriptions(t testing.TB) map[string]string {
	m := map[string]string{}
	for _, s := range sourceMatches(t, "server/cmd/builtin/description.go", `(?m)^\t"([\w-]+)":\s+"(commands\.[^"]+)",$`) {
		m[s[1]] = s[2]
	}
	if len(m) < 40 {
		t.Fatalf("found %d native descriptions", len(m))
	}
	return m
}

// descriptionTranslations returns the translation of the description of a
// builtin command, in the new code and in the old.
func descriptionTranslations(name string) (chat.Translation, oldTranslation) {
	k := "commands." + name + ".description"
	d := commandDescription{key: k}
	if name == "gamemode" {
		d.nativeKey = k
	}
	english := descriptionCatalog("en")[k]
	return chat.Translate(d, 0, english), oldTranslate(d, 0, english)
}

func TestChatRepoTranslations(t *testing.T) {
	news := newRepoTranslations(t)
	if len(news) != len(oldRepoTranslations) {
		t.Fatalf("%d translations, the old code had %d", len(news), len(oldRepoTranslations))
	}
	cases := 0
	for name, old := range oldRepoTranslations {
		nw, ok := news[name]
		if !ok {
			t.Fatalf("%s is gone", name)
		}
		for _, s := range []string{"Steve", "§cAlex", "", "100%", "a %v b", "<red>x</red> &amp;"} {
			// Each kind of argument in each place, the other places
			// strings.
			args := chatArgs(s, 3)
			for i := range max(old.params, 1) {
				for _, a := range args {
					newArgs, oldArgs := make([]any, old.params), make([]any, old.params)
					for j := range newArgs {
						newArgs[j], oldArgs[j] = s, s
					}
					if old.params > 0 {
						newArgs[i], oldArgs[i] = a.new, a.old
					}
					checkChat(t, fmt.Sprintf("%s, %s at %d, %q", name, a.name, i, s), nw, old, newArgs, oldArgs)
					cases++
				}
			}
		}
	}
	t.Logf("%d translations, %d cases", len(news), cases)
}

func TestChatCommandDescriptions(t *testing.T) {
	native := nativeDescriptions(t)
	cases := 0
	for k, english := range descriptionCatalog("en") {
		name := strings.TrimSuffix(strings.TrimPrefix(k, "commands."), ".description")
		d := commandDescription{key: k, nativeKey: native[name]}
		nw, old := chat.Translate(d, 0, english), oldTranslate(d, 0, english)
		// As the help command shows it, too.
		line := "/" + name + " - %v"
		checkChat(t, name, nw, old, nil, nil)
		checkChat(t, name+" in help", nw.Enc(line), old.Enc(line), nil, nil)
		cases += 2
		// The description AvailableCommands is sent: Message, as the
		// session reads it, gives what Resolve gave.
		for _, l := range chatLocales {
			msg, _, _ := Message(nw.F().Text(l))
			if want := old.Resolve(l); msg != want {
				t.Fatalf("%s, %v: description %q, the old code sent %q", name, l, msg, want)
			}
		}
	}
	if cases < 100 {
		t.Fatalf("only %d cases", cases)
	}
	t.Logf("%d cases", cases)
}

// TestChatInlineTranslations holds the fields that the Bedrock session writes
// with Inline, such as the reason of a disconnection, to what they were for
// a translation without arguments: the string Resolve of the old code gave,
// "§r§e%disconnect.disconnected§r" for the message of a server that shuts
// down.
func TestChatInlineTranslations(t *testing.T) {
	news := newRepoTranslations(t)
	cases := 0
	for name, old := range oldRepoTranslations {
		if old.params != 0 {
			continue
		}
		for _, l := range chatLocales {
			if got, want := Inline(news[name].F().Text(l)), old.Resolve(l); got != want {
				t.Fatalf("%s, %v: Inline gives %q, Resolve of the old code %q", name, l, got, want)
			}
			cases++
		}
	}
	if got := Inline(chat.MessageServerDisconnect.F().Text(language.English)); got != "§r§e%disconnect.disconnected§r" {
		t.Fatalf("the message of a server that shuts down is %q", got)
	}
	t.Logf("%d cases", cases)
}

func TestChatGoldenTranslation(t *testing.T) {
	// goldenTranslation of server/session/golden_text_test.go, sent with the
	// arguments the golden tests pass it.
	nw := chat.Translate(key("%commands.give.success"), 3, "Gave %v * %v to %v").Enc("<diamond>%v</diamond>")
	old := oldTranslate(key("%commands.give.success"), 3, "Gave %v * %v to %v").Enc("<diamond>%v</diamond>")
	checkChat(t, "golden", nw, old, []any{"§sdiamond", 3, chat.MessageServerDisconnect}, []any{"§sdiamond", 3, oldRepoTranslations["chat.MessageServerDisconnect"]})
	checkChat(t, "golden output", nw, old, []any{"a", "b", "c"}, []any{"a", "b", "c"})
}

// TestChatText checks that translations are the texts of the model: what
// a translation of the chat package is to a client that renders text of its
// own, such as a Java Edition client.
func TestChatText(t *testing.T) {
	steve, alex := text.Literal("Steve"), text.Literal("§cAlex")
	stone := chat.Translate(key("%tile.stone.name"), 0, "Stone")
	mob := chat.Translate(key("%death.attack.mob"), 2, "%v was slain by %v")
	for _, c := range []struct {
		name  string
		t     chat.Translation
		args  []any
		model text.Text
	}{
		{"join", chat.MessageJoin, []any{"Steve"},
			text.Colourf("<yellow>%v</yellow>", text.Translate("multiplayer.player.joined", "%s joined the game", steve))},
		{"no format", chat.MessageSleeping, []any{"Steve", 2},
			text.Colourf("%v", text.Translate("chat.type.sleeping", "%s is sleeping in a bed. To skip to dawn, %s more users need to sleep in beds at the same time.", steve, text.Literal("2")))},
		{"nested tags", chat.MessageAdminCommand, []any{"§cAlex", "Made it day"},
			text.Colourf("<grey><i>%v</i></grey>", text.Translate("chat.type.admin", "[%s: %s]", alex, text.Literal("Made it day")))},
		{"no parameters", chat.MessageBedTooFar, nil,
			text.Colourf("<grey>%v</grey>", text.Translate("tile.bed.tooFar", "Bed is too far away"))},
		{"a percent sign", chat.Translate(key("%k"), 0, "100% done"), nil,
			text.Colourf("%v", text.Translate("k", "100%% done"))},
		{"a translation as a parameter", mob, []any{"Steve", stone},
			text.Colourf("%v", text.Translate("death.attack.mob", "%s was slain by %s", steve, text.Colourf("%v", text.Translate("tile.stone.name", "Stone"))))},
		{"a mob as a parameter", mob, []any{"Steve", text.Translate("entity.zombie.name", "Zombie")},
			text.Colourf("%v", text.Translate("death.attack.mob", "%s was slain by %s", steve, text.Translate("entity.zombie.name", "Zombie")))},
		{"a text as a parameter", mob, []any{"Steve", text.Literal("x").WithStyle(text.Style{Colour: text.Red})},
			text.Colourf("%v", text.Translate("death.attack.mob", "%s was slain by %s", steve, text.Literal("x").WithStyle(text.Style{Colour: text.Red})))},
		{"a translation that takes arguments as a parameter", mob, []any{"Steve", chat.MessageJoin},
			text.Colourf("%v", text.Translate("death.attack.mob", "%s was slain by %s", steve, text.Translate("multiplayer.player.joined", "%s joined the game")))},
		{"server text", chat.Translate(key("Hello <red>%s</red>"), 1, "Hello %v"), []any{"Steve"},
			text.Colourf("%v", "Hello <red>%s</red>")},
	} {
		got := c.t.F(c.args...).Text(language.English)
		if !text.Equal(got, c.model) {
			t.Errorf("%s: %#v, want %#v", c.name, got, c.model)
		}
		var texter chat.Texter = c.t.F(c.args...)
		if !text.Equal(texter.Text(language.English), c.model) {
			t.Errorf("%s: the Texter differs", c.name)
		}
	}
	// A Translation that takes arguments as an argument made the old code
	// panic; it is now its key, which the client looks up.
	msg, params, _ := Message(mob.F("Steve", chat.MessageJoin).Text(language.English))
	if msg != "§r%death.attack.mob" || !slices.Equal(params, []string{"Steve", "%multiplayer.player.joined"}) {
		t.Errorf("a Translation that takes arguments as an argument: %q, %q", msg, params)
	}
	// Server text with arguments: no key and no parameters (Q1 of the
	// design of phase 1).
	if msg, params, translated := Message(chat.Translate(key("Hello"), 1, "Hello %v").F("x").Text(language.English)); msg != "§rHello" || params != nil || translated {
		t.Errorf("server text: %q, %q, %v", msg, params, translated)
	}
}

// TestChatMalformed checks that no fallback, Enc format or key makes a
// translation panic once F has the arguments it asks for.
func TestChatMalformed(t *testing.T) {
	pieces := []string{"%", "%v", "%s", "%d", "%%", "%1$s", "%2$s", "%9$s", "%!", "<red>", "</red>", "<", "&", "&amp;", "a", "§", "§c", ""}
	r := rand.New(rand.NewPCG(13, 14))
	random := func() string {
		var b strings.Builder
		for range r.IntN(6) {
			b.WriteString(pieces[r.IntN(len(pieces))])
		}
		return b.String()
	}
	for range 20000 {
		params := r.IntN(4)
		args := make([]any, params)
		for i := range args {
			args[i] = random()
		}
		tr := chat.Translate(key(random()), params, random()).Enc(random())
		f := tr.F(args...)
		for _, l := range chatLocales {
			_, _, _ = Message(f.Text(l))
		}
		_ = f.String()
	}
}

// keyBytes are the characters of the keys of FuzzChatTranslation.
const keyBytes = "abcdefghijklmnopqrstuvwxyz0123456789._"

// fallbackPieces are the pieces of the fallbacks of FuzzChatTranslation,
// %v aside: text, codes, %% and the marks of the fallbacks of the game.
var fallbackPieces = []string{"%%", "a", " ", "Steve", "§c", "§r", "§l", "x§", "§", "日本", "100", ".", ":", "'", "\"", ">>", "[", "]", "*", "\n", "\xff"}

// encPieces are the pieces Enc formats of FuzzChatTranslation are made of,
// around their %v.
var encPieces = []string{"<red>", "</red>", "<bold>", "</bold>", "<b>", "<i>", "</i>", "<grey>", "</grey>", "<yellow>", "</yellow>", "<diamond>", "a", " ", "> ", "[", "]", "§l", "/about - ", "&amp;"}

// FuzzChatTranslation compares translations of random keys, fallbacks, Enc
// formats and arguments with those of the old code, as checkChat does.
// Keys are keys after a % or, without, text of the server's own; fallbacks
// hold up to three %v, which set the number of parameters, and %%; Enc
// formats hold one %v in markup; the arguments are of every kind chatArgs
// returns.
func FuzzChatTranslation(f *testing.F) {
	f.Add("multiplayer.player.joined", true, []byte{0, 3, 4}, []byte{2, 9, 10}, []byte{0}, "Steve", 3)
	f.Add("chat.type.admin", true, []byte{14, 0, 15, 0, 16}, []byte{3, 7, 5, 8, 4}, []byte{1, 4}, "§cAlex", 0)
	f.Add("commands.generic.syntax", true, []byte{2, 0, 18, 0, 0, 0, 0}, []byte{1, 0, 1}, []byte{10, 11, 12}, "", -1)
	f.Add("death.attack.mob", true, []byte{0, 1, 2, 0}, []byte{}, []byte{0, 12}, "Victim", 7)
	f.Add("x", false, []byte{1, 2}, []byte{18}, []byte{}, "Hello <red>world</red>", 0)
	f.Add("x", false, []byte{0, 0}, []byte{0, 1, 2}, []byte{7, 8}, "100% a %v &amp;", 1)
	f.Add("tile.bed.tooFar", true, []byte{}, []byte{1, 7, 8}, []byte{}, "", 0)
	// A fallback without parameters with %% in it, shown as it is.
	f.Add("k", true, []byte{9, 21, 10, 21, 21}, []byte{}, []byte{}, "", 0)
	f.Fuzz(func(t *testing.T, k string, percent bool, fallback, enc, args []byte, s string, n int) {
		var resolved string
		if percent {
			b := []byte(k)
			for i, c := range b {
				b[i] = keyBytes[int(c)%len(keyBytes)]
			}
			if len(b) == 0 {
				b = []byte("k")
			}
			resolved = "%" + string(b)
		} else {
			// Server text: a % in front would make it a key.
			resolved = strings.TrimLeft(s, "%")
		}
		var fb strings.Builder
		params := 0
		for _, c := range fallback {
			switch {
			case c%8 == 0 && params < 3:
				fb.WriteString("%v")
				params++
			case c%8 != 0:
				fb.WriteString(fallbackPieces[int(c)%len(fallbackPieces)])
			}
		}
		if fb.Len() == 0 {
			fb.WriteString("x")
		}
		var format strings.Builder
		for i, c := range enc {
			if i == len(enc)/2 {
				format.WriteString("%v")
			}
			format.WriteString(encPieces[int(c)%len(encPieces)])
		}
		if len(enc) == 0 {
			format.WriteString("%v")
		}
		kinds := chatArgs(s, n)
		newArgs, oldArgs := make([]any, params), make([]any, params)
		for i := range newArgs {
			a := kinds[0]
			if i < len(args) {
				a = kinds[int(args[i])%len(kinds)]
			}
			newArgs[i], oldArgs[i] = a.new, a.old
		}
		nw := chat.Translate(key(resolved), params, fb.String()).Enc(format.String())
		old := oldTranslate(key(resolved), params, fb.String()).Enc(format.String())
		checkChat(t, fmt.Sprintf("%q, %q, %q", resolved, fb.String(), format.String()), nw, old, newArgs, oldArgs)
	})
}
