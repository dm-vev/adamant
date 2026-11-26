package perm_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/dm-vev/adamant/sdk/perm"
	"github.com/google/uuid"
)

func TestPermHierarchyWildcardsNegationAndPersistence(t *testing.T) {
	store := perm.New(nil)
	store.SetDefaultGroup("member")
	store.GrantGroup("member", "chat.send")
	store.GrantGroup("member", "shop.buy")

	store.Inherit("vip", "member")
	store.GrantGroup("vip", "shop.*")
	store.GrantGroup("vip", "-shop.admin")

	store.Inherit("admin", "vip")
	store.GrantGroup("admin", "*")

	alice := uuid.NewString()
	bob := uuid.NewString()
	store.AddGroup(alice, "vip")
	store.AddGroup(bob, "admin")
	store.Grant(bob, "-server.stop")

	if !store.Has(alice, "chat.send") {
		t.Fatal("alice should inherit chat.send from member")
	}
	if !store.Has(alice, "shop.sell") {
		t.Fatal("alice should match shop.* wildcard")
	}
	if store.Has(alice, "shop.admin") {
		t.Fatal("alice should be denied shop.admin via explicit negation")
	}
	if !store.Has(bob, "anything.at.all") {
		t.Fatal("bob should match * wildcard")
	}
	if store.Has(bob, "server.stop") {
		t.Fatal("bob should be denied server.stop via player-level negation")
	}

	if !slices.Equal(store.GroupsOf(alice), []string{"vip"}) {
		t.Fatalf("GroupsOf(alice) = %v", store.GroupsOf(alice))
	}

	path := filepath.Join(t.TempDir(), "perms.json")
	if err := store.SaveFile(path); err != nil {
		t.Fatalf("SaveFile: %v", err)
	}

	reloaded := perm.New(nil)
	if err := reloaded.LoadFile(path); err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if !reloaded.Has(alice, "shop.sell") || reloaded.Has(alice, "shop.admin") || reloaded.Has(bob, "server.stop") {
		t.Fatal("reloaded permissions did not match saved state")
	}
}
