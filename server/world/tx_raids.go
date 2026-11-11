package world

// Raids returns a transaction-scoped handle for the raids of the world.
func (tx *Tx) Raids() RaidTransaction {
	return RaidTransaction{tx: tx}
}

// RaidTransaction provides access to the village raids of the world within a transaction. Its methods are those of
// the transaction that concern raids, named without the word raid where the handle says it already: Raids().At,
// Raids().Leader, Raids().Tick.
type RaidTransaction struct {
	tx *Tx
}
