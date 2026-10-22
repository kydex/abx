// Package profile defines profile-domain invariants shared across layers.
package profile

import (
	"fmt"
)

// ValidateName checks the profile name used by both CLI parsing and host storage.
func ValidateName(name string) error {
	if len(name) < 1 || len(name) > 64 {
		return fmt.Errorf("invalid profile name %q: use 1-64 ASCII characters", name)
	}
	// Check the whole character set before the first-character rule. For a
	// name such as "../run" the first byte is an allowed dot, so an
	// interleaved check would report the start rule and hide the slash that
	// actually makes the name invalid.
	for _, c := range []byte(name) {
		if !letterOrDigit(c) && c != '.' && c != '_' && c != '-' {
			return fmt.Errorf("invalid profile name %q: only ASCII letters, digits, dot, underscore or hyphen are allowed", name)
		}
	}
	if first := name[0]; !letterOrDigit(first) {
		return fmt.Errorf("invalid profile name %q: must start with a letter or digit", name)
	}
	return nil
}

func letterOrDigit(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}
