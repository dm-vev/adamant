package packbuilder

import (
	"os"
	"path/filepath"
	"strings"
)

// buildLanguageFile creates a lang file and writes all of the language entries to the pack.
func buildLanguageFile(dir string, lang []string) error {
	// Create the texts directory if it does not exist to keep builds idempotent.
	if err := os.MkdirAll(filepath.Join(dir, "texts"), os.ModePerm); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "texts/en_US.lang"), []byte(strings.Join(lang, "\n")), 0666); err != nil {
		return err
	}
	return nil
}
