package session

// entityActionRow is a row of entityActionRows: an action of server/entity
// whose packet is an ActorEventPacket of the same event whatever the value of
// the action is, bar the data a row takes from a field, or an AnimatePacket of
// the same animation with a count.
//
// The names of the constants are those of the protocol (package v2225, the
// protocol the session is compiled against): the generated code names them,
// so that a misspelt one does not compile.
type entityActionRow struct {
	// Action is the name of the type in server/entity.
	Action string

	// Event is the name of the constant of ActorEvent that is the event of
	// the packet. Alt, if set, is the event instead when the bool field When
	// of the action is true.
	Event string
	Alt   string
	When  string

	// Data, if set, is the integer field of the action the event data is
	// taken from as it is. Ticks is the field of a time.Duration whose
	// milliseconds, divided by 50, are the event data.
	Data  string
	Ticks string

	// Animate is the name of the constant of AnimateAction that is the
	// action of an AnimatePacket instead of an event. Count is the integer
	// field of the action that is its data, as a float, and Default the
	// count of a count that is not positive.
	Animate string
	Count   string
	Default string
}

// entityActionRows holds the actions whose packets entity_action_switch_gen.go
// writes. The rest of the actions, those whose packets depend on more than a
// field read, are the cases of ViewEntityAction. Run go generate to write
// the switch from this table: it fails for a row of a type that is no
// world.EntityAction.
//
//go:generate go run ./internal/gentables -kind action
var entityActionRows = []entityActionRow{
	{Action: "CriticalHitAction", Animate: "AnimateActionCriticalHit", Count: "Count", Default: "55"},
	{Action: "EnchantedHitAction", Animate: "AnimateActionMagicCriticalHit", Count: "Count", Default: "15"},
	{Action: "DragonDeathAction", Event: "ActorEventDragonStartDeathAnim"},
	{Action: "WitchMagicAction", Event: "ActorEventWitchHatMagic"},
	{Action: "SilverfishMergeAction", Event: "ActorEventSilverfishMergeAnimation"},
	{Action: "ItemStackSizeAction", Event: "ActorEventUpdateStackSize", Data: "Count"},
	{Action: "WolfShakeAction", Event: "ActorEventShakeWetness"},
	{Action: "VibrationDetectedAction", Event: "ActorEventVibrationDetected"},
	{Action: "SquidInkAction", Event: "ActorEventSquidFleeing"},
	{Action: "ArrowShakeAction", Event: "ActorEventShake", Ticks: "Duration"},
	{Action: "FireworkExplosionAction", Event: "ActorEventFireworksExplode"},
	{Action: "TotemUseAction", Event: "ActorEventTalismanActivate"},
	{Action: "FishHookBubbleAction", Event: "ActorEventFishhookBubble"},
	{Action: "FishHookBiteAction", Event: "ActorEventFishhookHookTime"},
	{Action: "FishHookTeaseAction", Event: "ActorEventFishhookTease"},
	{Action: "TameAction", Event: "ActorEventTamingFailed", Alt: "ActorEventTamingSucceeded", When: "Succeeded"},
	{Action: "DrinkAction", Event: "ActorEventDrinkPotion", Alt: "ActorEventDrinkMilk", When: "Milk"},
	{Action: "EatGrassAction", Event: "ActorEventEatGrass"},
	{Action: "LoveHeartsAction", Event: "ActorEventLoveHearts"},
}
