package mcdb

import "testing"

func TestFallDamageSettingPersists(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !db.Settings().FallDamage {
		t.Fatal("new database has fall damage disabled")
	}
	settings := db.Settings()
	settings.FallDamage = false
	db.SaveSettings(settings)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	db, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if db.Settings().FallDamage {
		t.Fatal("fall damage setting was not persisted")
	}
}
