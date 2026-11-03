package v2193

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"testing"

	"github.com/dm-vev/adamant/net/bedrock/wire"
)

// autoCraftRequest is an ItemStackRequest, header first, that BDS 1.26.52.3
// (build 51798919) accepted: a recipe-book craft of sticks naming two oak
// planks as its ingredients, consuming two planks from inventory slot 0 and
// placing the sticks in slot 9. It is the request of the case
// inventory/consume-inventory-only of
// protocol/bedrock/session/testdata/bedrock_1_26_52_autocraft.json.
const autoCraftRequest = "93010145040b0dad0c01020101146d696e6563726166743a6f616b5f706c616e6b730001000101146d696e6563726166743a6f616b5f706c616e6b730001000505020c00001400000011130101010f6d696e6563726166743a737469636b000400000a00000000000000000000010101043c0032ddffffff0c0009000000000000000000"

// TestRecipeIngredientsAsBDSTakesThem decodes a recipe-book craft BDS
// accepted and encodes it back to the same bytes.
func TestRecipeIngredientsAsBDSTakesThem(t *testing.T) {
	raw, _ := hex.DecodeString(autoCraftRequest)
	if sum := sha256.Sum256(raw); hex.EncodeToString(sum[:]) != "8bf0ba4f642621f4c005931ed80594c3a56cb55df08e527cdc5987edeafd903e" {
		t.Fatal("the captured request changed")
	}
	body := raw[2:] // The header: packet 147.
	var pk ItemStackRequestPacket
	r := wire.NewReader(body)
	pk.Decode(r)
	if r.Err() != nil || r.Len() != 0 {
		t.Fatalf("decode: %v, %d bytes left", r.Err(), r.Len())
	}
	auto, ok := pk.Requests[0].Actions[0].(*AutoCraftRecipeStackRequestAction)
	if !ok {
		t.Fatalf("first action %T", pk.Requests[0].Actions[0])
	}
	planks := RecipeIngredient{Descriptor: &DefaultItemDescriptor{Name: "minecraft:oak_planks"}, Count: 1}
	if auto.RecipeNetworkID != 1581 || auto.NumberOfCrafts != 1 || !reflect.DeepEqual(auto.Ingredients, []RecipeIngredient{planks, planks}) {
		t.Fatalf("auto craft %+v", auto)
	}
	var w wire.Writer
	pk.Encode(&w)
	if !bytes.Equal(w.B, body) {
		t.Fatalf("encoded back differently\nBDS  % x\nours % x", body, w.B)
	}
}

// TestRecipeIngredients codes the ingredients of each kind and refuses a
// variant that its type byte contradicts.
func TestRecipeIngredients(t *testing.T) {
	for _, x := range []RecipeIngredient{
		{Descriptor: &InvalidItemDescriptor{}, Count: 1},
		{Descriptor: &DefaultItemDescriptor{Name: "minecraft:stick", MetadataValue: 2}, Count: 3},
		{Descriptor: &MoLangItemDescriptor{Expression: "q.any_tag('a')", Version: 10}, Count: 4},
		{Descriptor: &ItemTagItemDescriptor{Tag: "minecraft:planks"}, Count: 5},
	} {
		var w wire.Writer
		x.Encode(&w)
		var back RecipeIngredient
		r := wire.NewReader(w.B)
		back.Decode(r)
		if r.Err() != nil || r.Len() != 0 || !reflect.DeepEqual(back, x) {
			t.Errorf("%T: read back %+v, %v", x.Descriptor, back, r.Err())
		}
	}
	for _, b := range [][]byte{{3, 1}, {4, 4}} {
		r := wire.NewReader(append(b, 0, 0, 0))
		if new(RecipeIngredient).Decode(r); r.Err() == nil {
			t.Errorf("read an ingredient of % x", b)
		}
	}
}
