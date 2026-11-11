package world

// Maps returns a transaction-scoped handle for the maps of the world.
func (tx *Tx) Maps() MapTransaction {
	return MapTransaction{tx: tx}
}

// MapTransaction provides access to the map data of the world within a transaction. Its methods are those of the
// transaction that concern maps, named without the word map: Maps().Create, Maps().Update, Maps().ForEachFrame.
type MapTransaction struct {
	tx *Tx
}
