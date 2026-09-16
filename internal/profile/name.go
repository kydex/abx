// Package profile defines profile-domain invariants shared across layers.
package profile

import "fmt"

// ValidateName checks the profile name used by both CLI parsing and host storage.
func ValidateName(name string) error {
	if len(name) < 1 || len(name) > 64 {
		return fmt.Errorf("invalid profile name length: use 1-64 ASCII characters")
	}
	for i, c := range []byte(name) {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || i > 0 && (c == '.' || c == '_' || c == '-')) {
			return fmt.Errorf("invalid profile name %q: start with a letter or digit; use only ASCII letters, digits, dot, underscore or hyphen", name)
		}
	}
	return nil
}
