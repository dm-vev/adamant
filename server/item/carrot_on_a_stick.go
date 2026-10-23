package item

import "github.com/df-mc/dragonfly/server/world"

// CarrotOnAStick is an item that can be used to control saddled pigs.
type CarrotOnAStick struct{}

// MaxCount ...
func (CarrotOnAStick) MaxCount() int {
	return 1
}

// DurabilityInfo ...
func (CarrotOnAStick) DurabilityInfo() DurabilityInfo {
	return DurabilityInfo{
		MaxDurability: 25,
		BrokenItem:    simpleItem(NewStack(FishingRod{}, 1)),
	}
}

// Use boosts the pig the user steers, as FoodOnAStickItem.use does, at the
// cost of 7 durability.
func (CarrotOnAStick) Use(tx *world.Tx, user User, ctx *UseContext) bool {
	if boostVehicle(tx, user, "minecraft:pig") {
		ctx.DamageItem(7)
		return true
	}
	return false
}

// EncodeItem ...
func (CarrotOnAStick) EncodeItem() (name string, meta int16) {
	return "minecraft:carrot_on_a_stick", 0
}

// ItemSteerable is a vehicle that a rider steers with an item on a stick
// and boosts by using it, as Java's ItemSteerable.
type ItemSteerable interface {
	// Boost boosts the vehicle if rider steers it and it is not boosted
	// already. It reports if it did.
	Boost(tx *world.Tx, rider User) bool
}

// boostVehicle boosts the vehicle of the type passed that the user rides.
func boostVehicle(tx *world.Tx, user User, vehicleType string) bool {
	r, ok := user.(interface{ Riding() *world.EntityHandle })
	if !ok || r.Riding() == nil {
		return false
	}
	v, ok := r.Riding().Entity(tx)
	if !ok || v.H().Type().EncodeEntity() != vehicleType {
		return false
	}
	s, ok := v.(ItemSteerable)
	return ok && s.Boost(tx, user)
}
