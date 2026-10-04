package profile

import (
	"strings"
	"testing"
)

func TestValidateName(t *testing.T) {
	valid := []string{
		"a", "Z", "0", "agent", "Agent01", "a.b", "a_b", "a-b",
		strings.Repeat("a", 64),
	}
	for _, name := range valid {
		if err := ValidateName(name); err != nil {
			t.Errorf("ValidateName(%q): %v", name, err)
		}
	}

	invalid := []string{
		"", ".agent", "_agent", "-agent", "../bad", "a/b", "a b", "a\\b",
		"é", "a\x00b", strings.Repeat("a", 65),
	}
	for _, name := range invalid {
		if err := ValidateName(name); err == nil {
			t.Errorf("ValidateName(%q) accepted invalid name", name)
		}
	}
}

func FuzzValidateName(f *testing.F) {
	for _, seed := range []string{"a", "Agent01", "a.b", "", ".agent", "a/b", "é", "a\x00b", strings.Repeat("a", 65)} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, name string) {
		if err := ValidateName(name); err != nil {
			return
		}
		// Accepted names satisfy the documented necessary conditions.
		if len(name) < 1 || len(name) > 64 {
			t.Fatalf("accepted out-of-range length %d: %q", len(name), name)
		}
		for i := 0; i < len(name); i++ {
			if name[i] > 127 {
				t.Fatalf("accepted non-ASCII byte %#x: %q", name[i], name)
			}
		}
	})
}
