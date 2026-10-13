package main

import (
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

func TestMatchesServerMessageLiteralAndJSON(t *testing.T) {
	const expected = "ADAMANT_BLOCK_CATALOG_CAPTURE_COMPLETE"
	for _, test := range []struct {
		name string
		text packet.Text
		want bool
	}{
		{"plain", packet.Text{TextType: packet.TextTypeSystem, Message: expected}, true},
		{"BDS script", packet.Text{TextType: packet.TextTypeObject, Message: `{"rawtext":[{"text":"ADAMANT_BLOCK_CATALOG_CAPTURE_COMPLETE"}]}` + "\n"}, true},
		{"multiple components", packet.Text{TextType: packet.TextTypeObject, Message: `{"rawtext":[{"text":"ADAMANT_BLOCK_CATALOG_"},{"text":"CAPTURE_COMPLETE"}]}`}, true},
		{"substring", packet.Text{TextType: packet.TextTypeSystem, Message: "not " + expected}, false},
		{"translated", packet.Text{TextType: packet.TextTypeObject, Message: `{"rawtext":[{"translate":"ADAMANT_BLOCK_CATALOG_CAPTURE_COMPLETE"}]}`}, false},
		{"trailing object", packet.Text{TextType: packet.TextTypeObject, Message: `{"rawtext":[{"text":"ADAMANT_BLOCK_CATALOG_CAPTURE_COMPLETE"}]} {}`}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := matchesServerMessage(&test.text, expected); got != test.want {
				t.Fatalf("matched=%v, want %v", got, test.want)
			}
		})
	}
}
