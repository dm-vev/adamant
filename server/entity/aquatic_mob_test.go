package entity

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
)

func TestAquaticBucketRoundTrip(t *testing.T) {
	originalData := world.EntityData{Data: (AquaticMobConfig{
		ID: "minecraft:tropical_fish", Health: 2, MaxHealth: 3, Variant: 5, BucketVariant: 117506305,
		Age: -2400, healthSet: true,
	}).New()}
	original := TropicalFishType.Open(nil, nil, &originalData).(*TropicalFish)
	bucket := original.SaveToBucket(original.BucketItem())
	fishBucket, ok := bucket.Item().(item.FishBucket)
	if !ok || fishBucket.EntityID() != "minecraft:tropical_fish" {
		t.Fatalf("bucket = %#v, want tropical fish bucket", bucket.Item())
	}

	var restoredData world.EntityData
	TropicalFishType.DecodeNBT(fishBucket.Data, &restoredData)
	restored := TropicalFishType.Open(nil, nil, &restoredData).(*TropicalFish)
	if got := restored.Health(); got != 2 {
		t.Fatalf("restored health = %v, want 2", got)
	}
	if got := restored.behaviour().bucketVariant; got != 117506305 {
		t.Fatalf("restored bucket variant = %d, want 117506305", got)
	}
	if got := restored.behaviour().age; got != -2400 {
		t.Fatalf("restored age = %d, want -2400", got)
	}
	if !restored.FromBucket() {
		t.Fatal("restored fish was not marked as bucket-spawned")
	}
}
