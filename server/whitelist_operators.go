package server

import (
	"net"
	"strings"

	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
)

// whitelistOperatorsAllower composes a Whitelist with an ops list so that operators bypass whitelist enforcement.
//
// Lumi/Nukkit always permits operators to join even when the whitelist is enabled. This wrapper keeps the whitelist
// implementation focused on allowlist behaviour and adds the bypass logic in the server wiring.
type whitelistOperatorsAllower struct {
	whitelist *Whitelist
	operators *operatorsList
}

func (a whitelistOperatorsAllower) Allow(addr net.Addr, d login.IdentityData, c login.ClientData) (string, bool) {
	if a.whitelist == nil {
		return "", true
	}

	// Nukkit lower-cases the operator lookup before checking. The whitelist implementation already handles trimming
	// and empty values safely, so we keep the operator check minimal and defer to Whitelist.Allow for errors.
	if a.operators != nil {
		name := strings.TrimSpace(d.DisplayName)
		if name != "" && a.operators.isOperator(name) {
			return "", true
		}
	}

	return a.whitelist.Allow(addr, d, c)
}

// underlyingWhitelist exposes the configured whitelist to server.New without requiring public API changes.
func (a whitelistOperatorsAllower) underlyingWhitelist() *Whitelist {
	return a.whitelist
}

var _ Allower = whitelistOperatorsAllower{}
