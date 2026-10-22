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

// The message must name the rule that was actually broken: a name beginning
// with a non-ASCII letter breaks the ASCII rule, not the first-character rule.
func TestValidateNameMessages(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
	}{
		{"é", "only ASCII letters"},
		{"имя", "only ASCII letters"},
		{"../bad", "only ASCII letters"},
		{"a/b", "only ASCII letters"},
		{"_agent", "must start with a letter or digit"},
		{".agent", "must start with a letter or digit"},
		{"-agent", "must start with a letter or digit"},
	} {
		err := ValidateName(tc.name)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("ValidateName(%q) = %v, want %q", tc.name, err, tc.want)
		}
	}
	long := strings.Repeat("a", 65)
	err := ValidateName(long)
	if err == nil || !strings.Contains(err.Error(), long) || !strings.Contains(err.Error(), "1-64") {
		t.Fatalf("length error does not name the value and the rule: %v", err)
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
