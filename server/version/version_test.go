package version

import (
	"regexp"
	"slices"
	"testing"
)

// TestMinecraftNumbers checks the numbers of the release as the world format
// stores them.
func TestMinecraftNumbers(t *testing.T) {
	if got, want := MinecraftNumbers(), []int32{1, 26, 50, 0, 0}; !slices.Equal(got, want) {
		t.Fatalf("MinecraftNumbers() = %v, want %v", got, want)
	}
}

// TestJavaFormat checks that the Java release is written as major and minor
// numbers, the form Java Edition names its releases by since 26.1.
func TestJavaFormat(t *testing.T) {
	if !regexp.MustCompile(`^[1-9][0-9]*\.[0-9]+$`).MatchString(Java) {
		t.Fatalf("Java = %q, want major.minor", Java)
	}
}
