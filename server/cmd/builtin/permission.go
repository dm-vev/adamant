package builtin

import (
	"fmt"
	"strings"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/client"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
)

// permissionListCmd lists the operators of ops.json with their levels and
// the roles of permissions.json, as BDS's /permission list does.
type permissionListCmd struct {
	admin
	srv  serverAdapter
	List cmd.SubCommand `cmd:"list"`
}

// permissionReloadCmd reads ops.json and permissions.json again and
// refreshes every player online, as BDS's /permission reload does.
type permissionReloadCmd struct {
	admin
	srv    serverAdapter
	Reload cmd.SubCommand `cmd:"reload"`
}

// permissionSetCmd gives a player a role of permissions.json: visitor,
// member, or custom with the abilities named; default takes its entry off,
// so that it has the role of an operator or the DefaultRole again.
// BDS's /permission has no set.
type permissionSetCmd struct {
	admin
	srv       serverAdapter
	Set       cmd.SubCommand            `cmd:"set"`
	Name      string                    `cmd:"player"`
	Role      permissionRole            `cmd:"role"`
	Abilities cmd.Optional[cmd.Varargs] `cmd:"abilities"`
}

// permissionRole is a role /permission set gives. The operator role is
// /op's.
type permissionRole string

func (permissionRole) Type() string { return "permissionrole" }
func (permissionRole) Options(cmd.Source) []string {
	return []string{"visitor", "member", "custom", "default"}
}

func newPermissionCommand(srv serverAdapter) cmd.Command {
	return newCommand("permission", nil, permissionListCmd{srv: srv}, permissionReloadCmd{srv: srv},
		permissionSetCmd{srv: srv})
}

func (c permissionListCmd) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	ops, err := c.srv.OperatorList()
	if err != nil {
		o.Error(err)
		return
	}
	roles, err := c.srv.RoleEntries()
	if err != nil {
		o.Error(err)
		return
	}
	if len(ops) == 0 {
		o.Print("There are no operators")
	} else {
		o.Printf("There are %d operator(s):", len(ops))
		for _, op := range ops {
			line := fmt.Sprintf("%s: level %v", op.Player, op.Level)
			if !op.OwnLevel {
				line += " (OperatorLevel)"
			}
			if op.BypassesPlayerLimit {
				line += ", bypasses the player limit"
			}
			o.Print(line)
		}
	}
	if len(roles) == 0 {
		o.Print("There are no roles in permissions.json")
		return
	}
	o.Printf("There are %d role(s) in permissions.json:", len(roles))
	for _, r := range roles {
		o.Print(r)
	}
}

func (c permissionReloadCmd) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) {
	if err := c.srv.ReloadPermissions(); err != nil {
		o.Error(err)
		return
	}
	o.Print("Permissions reloaded from file.")
}

func (c permissionSetCmd) Run(src cmd.Source, o *cmd.Output, _ *world.Tx) {
	names := strings.Fields(reasonArg(c.Abilities))
	if strings.EqualFold(string(c.Role), "default") {
		if len(names) > 0 {
			o.Error("Only a custom role takes abilities.")
			return
		}
		c.clear(src, o)
		return
	}
	role, err := client.ParseRole(string(c.Role))
	if err != nil || role == client.RoleOperator {
		o.Errort(cmd.MessageParameterInvalid, string(c.Role))
		return
	}
	var abilities client.Abilities
	if role == client.RoleCustom {
		if abilities, err = client.ParseAbilities(names); err != nil {
			o.Error(err)
			return
		}
	} else if len(names) > 0 {
		o.Error("Only a custom role takes abilities.")
		return
	}
	t, ok := addTarget(c.srv, o, c.Name)
	if !ok || !c.mayChange(src, o, t) {
		return
	}
	shown := role.String()
	if role == client.RoleCustom {
		shown += fmt.Sprintf(" %v", abilities.Names())
	}
	changed, err := c.srv.SetRole(t.Arg(), role, abilities)
	switch {
	case err != nil:
		o.Error(err)
	case !changed:
		o.Printf("Nothing changed. %s already has the role %s", t, shown)
	case t.Pending:
		o.Printf(pendingAdded, t.Name, t.Name, "gets the role "+shown)
	default:
		o.Printf("Set the role of %s to %s.", t, shown)
	}
}

// clear takes the entry of the player named off permissions.json.
func (c permissionSetCmd) clear(src cmd.Source, o *cmd.Output) {
	t, ok := removeTarget(c.srv, o, server.Roles, c.Name)
	if !ok || !c.mayChange(src, o, t) {
		return
	}
	removed, err := c.srv.ClearRole(t.Arg())
	switch {
	case err != nil:
		o.Error(err)
	case !removed:
		o.Printf("Nothing changed. %s has no role in permissions.json", t)
	default:
		o.Printf("%s has the default role again.", t)
	}
}

// mayChange reports whether src may change the role of the player of t,
// and prints why not: no one changes the role of an operator above their
// own level.
func (c permissionSetCmd) mayChange(src cmd.Source, o *cmd.Output, t server.Target) bool {
	if level, ok := c.srv.OperatorLevel(t.Arg()); ok {
		if own := cmd.SourceLevel(src); level > own {
			o.Errorf("You can't change the role of an operator above your own level (%v).", own)
			return false
		}
	}
	return true
}
