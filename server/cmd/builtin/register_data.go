package builtin

import "github.com/df-mc/dragonfly/server/cmd"

// registerDataCommands registers Java 26.3 execute, data, item, loot and scheduling commands.
func registerDataCommands(srv serverAdapter) {
	cmd.Register(cmd.New("execute", "Executes a command with a modified context.", nil, executeCommand{}))
	cmd.Register(cmd.New("data", "Gets or modifies block and storage data.", nil, dataCommand{}))
	cmd.Register(newLootCommand())
	cmd.Register(newReturnCommand())
	cmd.Register(newItemCommand())
	_ = srv
}
