package world

import "github.com/google/uuid"

// resolveEntityRiding publishes saved passenger links only after both actors
// enter the same world. Loading a rider before its vehicle does not load chunks.
func (w *World) resolveEntityRiding(handle *EntityHandle) {
	if id := handle.pendingVehicle; id != uuid.Nil {
		if vehicle, ok := w.entitiesByUUID[id]; ok && vehicle != handle {
			handle.data.Riding, handle.pendingVehicle = vehicle, uuid.Nil
		} else {
			if w.pendingRiders == nil {
				w.pendingRiders = make(map[uuid.UUID][]*EntityHandle)
			}
			w.pendingRiders[id] = append(w.pendingRiders[id], handle)
		}
	}
	for _, rider := range w.pendingRiders[handle.id] {
		if _, loaded := w.entities[rider]; loaded && rider != handle && rider.pendingVehicle == handle.id {
			rider.data.Riding, rider.pendingVehicle = handle, uuid.Nil
		}
	}
	delete(w.pendingRiders, handle.id)
}

func (w *World) removePendingRider(handle *EntityHandle) {
	id := handle.pendingVehicle
	if id == uuid.Nil {
		return
	}
	riders := w.pendingRiders[id]
	for index, rider := range riders {
		if rider == handle {
			copy(riders[index:], riders[index+1:])
			riders[len(riders)-1] = nil
			riders = riders[:len(riders)-1]
			break
		}
	}
	if len(riders) == 0 {
		delete(w.pendingRiders, id)
	} else {
		w.pendingRiders[id] = riders
	}
}
