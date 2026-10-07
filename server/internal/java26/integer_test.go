package java26

import "testing"

func TestParseSignedIntegerRadixAndBMPDigits(t *testing.T) {
	cases := []struct {
		text  string
		radix int
		bits  int
		want  int64
	}{
		{"+1", 10, 32, 1},
		{"01", 10, 32, 1},
		{"\u0661\u0662", 10, 32, 12},
		{"\uff21", 16, 32, 10},
		{"-ff", 16, 32, -255},
	}
	for _, tc := range cases {
		got, err := ParseSignedInteger(tc.text, tc.radix, tc.bits)
		if err != nil || got != tc.want {
			t.Errorf("ParseSignedInteger(%q, %d, %d) = %d, %v; want %d", tc.text, tc.radix, tc.bits, got, err, tc.want)
		}
	}
}

func TestParseSignedIntegerRejectsJavaInvalidDigitsAndOverflow(t *testing.T) {
	for _, tc := range []struct {
		text        string
		radix, bits int
	}{
		{"𝟙", 10, 32}, // Supplementary digits are two UTF-16 chars, neither a digit.
		{"2147483648", 10, 32},
		{"-2147483649", 10, 32},
		{"2", 2, 32},
		{"1", 1, 32},
		{"1", 10, 0},
	} {
		if _, err := ParseSignedInteger(tc.text, tc.radix, tc.bits); err == nil {
			t.Errorf("ParseSignedInteger(%q, %d, %d) unexpectedly succeeded", tc.text, tc.radix, tc.bits)
		}
	}
}
