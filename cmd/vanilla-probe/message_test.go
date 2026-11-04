package main

import (
	"testing"

	mc "github.com/dm-vev/adamant/net/bedrock/proto/v2225"
)

func TestMatchesServerMessageLiteralAndJSON(t *testing.T) {
	const expected = "ADAMANT_BLOCK_CATALOG_CAPTURE_COMPLETE"
	for _, test := range []struct {
		name string
		text mc.TextPacket
		want bool
	}{
		{"plain", mc.TextPacket{TextType: mc.TextTypeSystem, Message: expected}, true},
		{"BDS script", mc.TextPacket{TextType: mc.TextTypeObject, Message: `{"rawtext":[{"text":"ADAMANT_BLOCK_CATALOG_CAPTURE_COMPLETE"}]}` + "\n"}, true},
		{"multiple components", mc.TextPacket{TextType: mc.TextTypeObject, Message: `{"rawtext":[{"text":"ADAMANT_BLOCK_CATALOG_"},{"text":"CAPTURE_COMPLETE"}]}`}, true},
		{"substring", mc.TextPacket{TextType: mc.TextTypeSystem, Message: "not " + expected}, false},
		{"translated", mc.TextPacket{TextType: mc.TextTypeObject, Message: `{"rawtext":[{"translate":"ADAMANT_BLOCK_CATALOG_CAPTURE_COMPLETE"}]}`}, false},
		{"trailing object", mc.TextPacket{TextType: mc.TextTypeObject, Message: `{"rawtext":[{"text":"ADAMANT_BLOCK_CATALOG_CAPTURE_COMPLETE"}]} {}`}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := matchesServerMessage(&test.text, expected); got != test.want {
				t.Fatalf("matched=%v, want %v", got, test.want)
			}
		})
	}
}
