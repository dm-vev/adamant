package packbuilder

import "strings"

// identifierName returns the name part of a namespaced identifier, or false if the identifier is invalid.
func identifierName(identifier string) (string, bool) {
	if identifier == "" {
		return "", false
	}
	if _, name, ok := strings.Cut(identifier, ":"); ok {
		if name == "" {
			return "", false
		}
		return name, true
	}
	return identifier, true
}
