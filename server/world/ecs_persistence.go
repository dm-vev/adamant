package world

import (
	"errors"
	"fmt"

	"github.com/df-mc/goleveldb/leveldb"
	"github.com/dm-vev/adamant/net/nbt"
)

func (w *World) ecsComponentsNamespace() string {
	id, _ := DimensionID(w.conf.Dim)
	return fmt.Sprintf("ecs-components:%d", id)
}

func (w *World) loadECSComponents() {
	data, err := w.LoadWorldData(w.ecsComponentsNamespace())
	if errors.Is(err, leveldb.ErrNotFound) {
		return
	}
	if err != nil {
		w.log.Error("load world ECS components: " + err.Error())
		return
	}
	w.ecsComponentsPersisted.Store(true)
	if len(data) == 0 {
		return
	}
	comps := make(map[string]any)
	if err := nbt.UnmarshalEncoding(data, &comps, nbt.LittleEndian); err != nil {
		w.log.Error("decode world ECS components: " + err.Error())
		return
	}
	w.components.DecodeNBT(comps)
}

func (w *World) encodeECSComponents() ([]byte, bool) {
	comps := w.components.EncodeNBT()
	if len(comps) == 0 {
		if !w.ecsComponentsPersisted.Load() {
			return nil, false
		}
		return []byte{}, true
	}
	b, err := nbt.MarshalEncoding(comps, nbt.LittleEndian)
	if err != nil {
		w.saveLog.Error("encode world ECS components: " + err.Error())
		return nil, false
	}
	w.ecsComponentsPersisted.Store(true)
	return b, true
}
