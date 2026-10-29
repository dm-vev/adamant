package builtin

import "testing"

func TestComponentText(t *testing.T) {
	got := componentText(`{"text":"Hello","color":"red","bold":true,"extra":[{"text":" world","italic":true}]}`)
	want := "Hello§c§l world§o"
	if got != want {
		t.Fatalf("componentText() = %q, want %q", got, want)
	}
	got = componentText(`{"translate":"chat.type.text","with":["Alex",{"text":"hello"}]}`)
	if got != "chat.type.text Alex hello" {
		t.Fatalf("translated component = %q", got)
	}
}

// TestComponentTextRawText renders the rawtext components of Bedrock's
// tellraw, which once rendered empty: a Text without a message, whose
// encoding took the server down.
func TestComponentTextRawText(t *testing.T) {
	if got := componentText(`{"rawtext":[{"text":"Hello"},{"text":" world"}]}`); got != "Hello world" {
		t.Fatalf("componentText() = %q, want %q", got, "Hello world")
	}
}
