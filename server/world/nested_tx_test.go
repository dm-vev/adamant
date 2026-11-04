package world

import (
	"testing"

	"github.com/go-gl/mathgl/mgl64"
)

// bindingEntityType is an entity type whose entities are bound to each
// transaction they are used in, as players are.
type bindingEntityType struct{ testEntityType }

func (bindingEntityType) Open(_ *Tx, handle *EntityHandle, data *EntityData) Entity {
	return &bindingEntity{testEntity: testEntity{handle: handle, data: data}}
}

// bindingEntity is an entity of bindingEntityType: it keeps the transaction
// it was bound to last.
type bindingEntity struct {
	testEntity
	tx *Tx
}

func (e *bindingEntity) BindTransaction(tx *Tx) { e.tx = tx }

// TestNestedTransactionBindsEntitiesBack checks the transactions a
// synchronous World runs at once inside another: EntityHandle.Do and
// World.Exec from a transaction, also one in the other. An entity the inner
// one binds is bound back to the outer one when the inner ends, so that the
// outer goes on using it; it was left bound to the inner one, which had
// ended, and its next use in the outer one panicked.
func TestNestedTransactionBindsEntitiesBack(t *testing.T) {
	w := Config{Synchronous: true, RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	h := EntitySpawnOpts{Position: mgl64.Vec3{0.5, 0, 0.5}}.New(bindingEntityType{}, testEntityConfig{})
	<-w.Exec(func(tx *Tx) { tx.AddEntity(h) })
	boundTo := func(tx *Tx) *bindingEntity {
		t.Helper()
		e, ok := h.Entity(tx)
		if !ok {
			t.Fatal("the entity is not in the world")
		}
		return e.(*bindingEntity)
	}

	<-w.Exec(func(tx *Tx) {
		e := boundTo(tx)
		ran := false
		h.Do(func(inner *Tx, ie Entity) {
			ran = true
			if ie != e || e.tx != inner {
				t.Errorf("Do runs with %p bound to %p, want %p bound to the transaction of Do", ie, e.tx, e)
			}
			<-w.Exec(func(innermost *Tx) { boundTo(innermost) })
			if e.tx != inner {
				t.Error("after an Exec in Do, the entity is not bound to the transaction of Do")
			}
		})
		if !ran {
			t.Fatal("Do did not run at once on a synchronous world")
		}
		if e.tx != tx {
			t.Fatal("after Do, the entity is not bound to the transaction it ran in")
		}
		// The use of the outer transaction after Do.
		_ = e.tx.World()

		<-w.Exec(func(inner *Tx) { boundTo(inner) })
		if e.tx != tx {
			t.Error("after an Exec, the entity is not bound to the transaction it ran in")
		}
	})
	<-w.Exec(func(tx *Tx) {
		if e := boundTo(tx); e.tx != tx {
			t.Error("the next transaction does not bind the entity")
		}
	})
}

// TestTransactionLeavesUnnestedBindings checks that a transaction that
// ends binds back no entity another transaction bound before it on an
// asynchronous World: its transactions run one after another, each binding
// the entities it uses.
func TestTransactionLeavesUnnestedBindings(t *testing.T) {
	w := Config{RandomTickSpeed: -1}.New()
	t.Cleanup(func() { _ = w.Close() })
	h := EntitySpawnOpts{Position: mgl64.Vec3{0.5, 0, 0.5}}.New(bindingEntityType{}, testEntityConfig{})
	<-w.Exec(func(tx *Tx) { tx.AddEntity(h) })
	var e *bindingEntity
	var second *Tx
	<-w.Exec(func(tx *Tx) {
		ent, _ := h.Entity(tx)
		e = ent.(*bindingEntity)
	})
	<-w.Exec(func(tx *Tx) {
		second = tx
		_, _ = h.Entity(tx)
	})
	<-w.Exec(func(*Tx) {})
	if e.tx != second {
		t.Error("the entity is not bound to the last transaction that used it")
	}
}
