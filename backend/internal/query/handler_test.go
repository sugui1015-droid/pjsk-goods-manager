package query

import "testing"

func TestNormalizeCN(t *testing.T) {
	tests := map[string]string{
		"  Succ  ":     "Succ",
		"a   b\tc":     "a b c",
		"墓靑(ねこ)  neko": "墓靑(ねこ) neko",
	}
	for input, want := range tests {
		if got := normalizeCN(input); got != want {
			t.Fatalf("normalizeCN(%q) = %q, want %q", input, got, want)
		}
	}
}
