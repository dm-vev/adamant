package builtin

// registerWorldCommands registers Java 26.3 world, block and dimension commands.
func registerWorldCommands(srv serverAdapter) {
	registerWorldCoreCommands(srv)
	registerWorldTickCommands(srv)
}
