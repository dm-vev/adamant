package world

// SetAutoSaveEnabled controls periodic world saves. Explicit Save calls are unaffected.
func (w *World) SetAutoSaveEnabled(enabled bool) {
	if w == nil {
		return
	}
	w.autoSaveDisabled.Store(!enabled)
}

// AutoSaveEnabled reports whether periodic world saves are enabled.
func (w *World) AutoSaveEnabled() bool {
	return w != nil && !w.autoSaveDisabled.Load()
}
