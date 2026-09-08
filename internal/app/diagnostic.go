// Package diagnostic renders untrusted values safely for terminal diagnostics.
package app

import (
	"fmt"
	"strings"
	"unicode"
)

// Escape preserves printable text while escaping terminal-ambiguous characters.
func Escape(value string) string {
	var safe strings.Builder
	for _, r := range value {
		switch r {
		case '\a':
			safe.WriteString(`\a`)
		case '\b':
			safe.WriteString(`\b`)
		case '\f':
			safe.WriteString(`\f`)
		case '\n':
			safe.WriteString(`\n`)
		case '\r':
			safe.WriteString(`\r`)
		case '\t':
			safe.WriteString(`\t`)
		case '\v':
			safe.WriteString(`\v`)
		default:
			switch {
			case unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp):
				if r <= 0xffff {
					fmt.Fprintf(&safe, `\u%04x`, r)
				} else {
					fmt.Fprintf(&safe, `\U%08x`, r)
				}
			default:
				safe.WriteRune(r)
			}
		}
	}
	return safe.String()
}
