package world

import (
 "testing"

 "github.com/google/uuid"
)

func TestJavaUUIDSurvivesNativeEntityReload(t *testing.T) {
 id:=uuid.MustParse("67491480-7a42-4168-980a-c6906b09f268")
 original:=EntitySpawnOpts{ID:id}.New(testEntityType{},testEntityConfig{})
 encoded:=original.encodeNBT()
 reloaded:=entityFromData(testEntityType{},original.UniqueID(),encoded)
 if reloaded.UUID()!=id { t.Fatalf("reloaded UUID=%v, want %v",reloaded.UUID(),id) }
 if reloaded.UniqueID()!=original.UniqueID() { t.Fatal("Java UUID restoration changed native UniqueID") }
 for _,h:=range []*EntityHandle{original,reloaded} { h.Close() }
}

func TestEntityIdentityIndexesFollowWorldMembership(t *testing.T) {
 first:=Config{Synchronous:true}.New()
 second:=Config{Synchronous:true}.New()
 defer first.Close()
 defer second.Close()
 h:=EntitySpawnOpts{}.New(testEntityType{},testEntityConfig{})
 <-first.Exec(func(tx *Tx){
  tx.AddEntity(h)
  byUUID,ok:=tx.EntityByUUID(h.UUID());if !ok||byUUID.H()!=h{t.Fatal("added entity not resolvable by UUID")}
  byID,ok:=tx.EntityByUniqueID(h.UniqueID());if !ok||byID.H()!=h{t.Fatal("added entity not resolvable by native ID")}
  tx.RemoveEntity(byUUID)
  if _,ok:=tx.EntityByUUID(h.UUID());ok{t.Fatal("removed entity retained UUID entry")}
  if _,ok:=tx.EntityByUniqueID(h.UniqueID());ok{t.Fatal("removed entity retained native ID entry")}
 })
 <-second.Exec(func(tx *Tx){
  tx.AddEntity(h)
  if e,ok:=tx.EntityByUUID(h.UUID());!ok||e.H()!=h{t.Fatal("transferred entity not resolvable by UUID")}
  if e,ok:=tx.EntityByUniqueID(h.UniqueID());!ok||e.H()!=h{t.Fatal("transferred entity not resolvable by native ID")}
 })
}
