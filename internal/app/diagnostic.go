// Package app renders untrusted values safely for terminal diagnostics.
package app

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Escape preserves printable text while escaping terminal-ambiguous characters.
// An invalid UTF-8 byte prints as \xNN, so a value stays recoverable; only a
// real replacement character passes through as itself.
func Escape(value string) string {
	var safe strings.Builder
	for i := 0; i < len(value); {
		r, size := utf8.DecodeRuneInString(value[i:])
		if r == utf8.RuneError && size == 1 {
			fmt.Fprintf(&safe, `\x%02x`, value[i])
			i++
			continue
		}
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
		i += size
	}
	return safe.String()
}
