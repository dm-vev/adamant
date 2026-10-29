package main

import (
	"testing"

	"github.com/dm-vev/adamant/net/bedrock/proto/v2193"
)

func TestMatchesServerMessageLiteralAndJSON(t *testing.T) {
	const expected = "ADAMANT_BLOCK_CATALOG_CAPTURE_COMPLETE"
	for _, test := range []struct {
		name string
		text v2193.TextPacket
		want bool
	}{
		{"plain", v2193.TextPacket{TextType: v2193.TextTypeSystem, Message: expected}, true},
		{"BDS script", v2193.TextPacket{TextType: v2193.TextTypeObject, Message: `{"rawtext":[{"text":"ADAMANT_BLOCK_CATALOG_CAPTURE_COMPLETE"}]}` + "\n"}, true},
		{"multiple components", v2193.TextPacket{TextType: v2193.TextTypeObject, Message: `{"rawtext":[{"text":"ADAMANT_BLOCK_CATALOG_"},{"text":"CAPTURE_COMPLETE"}]}`}, true},
		{"substring", v2193.TextPacket{TextType: v2193.TextTypeSystem, Message: "not " + expected}, false},
		{"translated", v2193.TextPacket{TextType: v2193.TextTypeObject, Message: `{"rawtext":[{"translate":"ADAMANT_BLOCK_CATALOG_CAPTURE_COMPLETE"}]}`}, false},
		{"trailing object", v2193.TextPacket{TextType: v2193.TextTypeObject, Message: `{"rawtext":[{"text":"ADAMANT_BLOCK_CATALOG_CAPTURE_COMPLETE"}]} {}`}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := matchesServerMessage(&test.text, expected); got != test.want {
				t.Fatalf("matched=%v, want %v", got, test.want)
			}
		})
	}
}
