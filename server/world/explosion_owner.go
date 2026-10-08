package world

// ExplosionCausingEntity resolves an explosion's living source or the loaded
// owner of its projectile/primed explosive. An unloaded owner is not replaced
// with the explosive itself.
func ExplosionCausingEntity(source ExplosionSource, tx *Tx) Entity {
	var entity Entity
	switch source := source.(type) {
	case EntityExplosionSource:
		entity = source.Entity
	case *EntityExplosionSource:
		entity = source.Entity
	}
	if owned, ok := entity.(interface{ Owner() *EntityHandle }); ok {
		if handle := owned.Owner(); handle != nil {
			entity, _ = tx.EntityByUUID(handle.UUID())
			return entity
		}
		return nil
	}
	return entity
}
