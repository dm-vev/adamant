package cmd

import "testing"

// Brigadier's StringReader.readInt and readDouble read only the characters
// 0-9, '.' and '-' (StringReader.isAllowedNumber) before Integer.parseInt or
// Double.parseDouble, and an argument must then end at whitespace. So signs,
// exponents, hex, NaN and Infinity are syntax errors in Java.
func TestNumberArgumentsFollowBrigadier(t *testing.T) {
	ints := []struct {
		in   string
		want int
		ok   bool
	}{
		{"0", 0, true},
		{"42", 42, true},
		{"-7", -7, true},
		{"2147483647", 2147483647, true},
		{"", 0, false},
		{"1.5", 0, false},
		{"+5", 0, false},
		{"1e3", 0, false},
		{"0x10", 0, false},
		{"5a", 0, false},
	}
	for _, c := range ints {
		got, err := parseOne[int](t, c.in)
		if (err == nil) != c.ok || got != c.want {
			t.Errorf("int %q = %d, %v; want %d, ok=%v", c.in, got, err, c.want, c.ok)
		}
	}
	if _, err := parseOne[int8](t, "128"); err == nil {
		t.Error("int8 128 was accepted; Brigadier rejects values out of the type's range")
	}
	if _, err := parseOne[uint](t, "-1"); err == nil {
		t.Error("uint -1 was accepted")
	}

	floats := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"1", 1, true},
		{"-2.5", -2.5, true},
		{".5", 0.5, true},
		{"-.5", -0.5, true},
		{"3.", 3, true},
		{"", 0, false},
		{"-", 0, false},
		{"1-2", 0, false},
		{"+1", 0, false},
		{"1e3", 0, false},
		{"0x1p4", 0, false},
		{"NaN", 0, false},
		{"Inf", 0, false},
		{"-Infinity", 0, false},
	}
	for _, c := range floats {
		got, err := parseOne[float64](t, c.in)
		if (err == nil) != c.ok || got != c.want {
			t.Errorf("float %q = %v, %v; want %v, ok=%v", c.in, got, err, c.want, c.ok)
		}
	}
}

// BoolArgumentType reads an unquoted string and accepts exactly "true" or
// "false" (StringReader.readBoolean), case-sensitively.
func TestBooleanArgumentsFollowBrigadier(t *testing.T) {
	for in, want := range map[string]bool{"true": true, "false": false} {
		if got, err := parseOne[bool](t, in); err != nil || got != want {
			t.Errorf("bool %q = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"1", "0", "t", "f", "TRUE", "False", "yes", ""} {
		if _, err := parseOne[bool](t, in); err == nil {
			t.Errorf("bool %q was accepted", in)
		}
	}
}
