package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
)

func TestWhitelistOperatorsBypass(t *testing.T) {
	dir := t.TempDir()
	whitelistPath := filepath.Join(dir, "white-list.txt")
	opsPath := filepath.Join(dir, "ops.txt")

	if err := os.WriteFile(whitelistPath, []byte("bob\r\n"), 0644); err != nil {
		t.Fatalf("write whitelist: %v", err)
	}
	if err := os.WriteFile(opsPath, []byte("alice\r\n"), 0644); err != nil {
		t.Fatalf("write ops: %v", err)
	}

	wl, err := LoadWhitelist(whitelistPath)
	if err != nil {
		t.Fatalf("load whitelist: %v", err)
	}
	wl.SetEnabled(true)

	ops, err := loadOperators(opsPath)
	if err != nil {
		t.Fatalf("load ops: %v", err)
	}

	allower := whitelistOperatorsAllower{whitelist: wl, operators: ops}

	if msg, ok := allower.Allow(nil, login.IdentityData{DisplayName: "Alice"}, login.ClientData{}); !ok {
		t.Fatalf("expected operator to bypass whitelist, got reject message %q", msg)
	}
	if msg, ok := allower.Allow(nil, login.IdentityData{DisplayName: "bob"}, login.ClientData{}); !ok {
		t.Fatalf("expected whitelisted player to be allowed, got reject message %q", msg)
	}
	if msg, ok := allower.Allow(nil, login.IdentityData{DisplayName: "charlie"}, login.ClientData{}); ok {
		t.Fatalf("expected non-whitelisted player to be rejected, got %q", msg)
	}
}

func TestWhitelistOperatorsEmptyNameRejects(t *testing.T) {
	dir := t.TempDir()
	whitelistPath := filepath.Join(dir, "white-list.txt")
	opsPath := filepath.Join(dir, "ops.txt")

	if err := os.WriteFile(whitelistPath, []byte(""), 0644); err != nil {
		t.Fatalf("write whitelist: %v", err)
	}
	if err := os.WriteFile(opsPath, []byte("alice\r\n"), 0644); err != nil {
		t.Fatalf("write ops: %v", err)
	}

	wl, err := LoadWhitelist(whitelistPath)
	if err != nil {
		t.Fatalf("load whitelist: %v", err)
	}
	wl.SetEnabled(true)

	ops, err := loadOperators(opsPath)
	if err != nil {
		t.Fatalf("load ops: %v", err)
	}

	allower := whitelistOperatorsAllower{whitelist: wl, operators: ops}
	if msg, ok := allower.Allow(nil, login.IdentityData{DisplayName: ""}, login.ClientData{}); ok {
		t.Fatalf("expected empty name to be rejected, got %q", msg)
	}
}
