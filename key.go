package sequence

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	maxNameLen  = 128
	maxScopeLen = 128
)

// Key identifies one counter: a series name plus an optional scope such as a tenant.
// The zero Scope is the global counter. Key is comparable and safe as a map key.
type Key struct {
	Name  string // required
	Scope string // optional; empty means global
}

// String returns a flat, injective form for stores that need a single string: distinct
// keys never produce equal strings. "%" and "/" are escaped in both parts, and the
// separator is omitted when Scope is empty.
func (k Key) String() string {
	if k.Scope == "" {
		return escapeKeyPart(k.Name)
	}
	return escapeKeyPart(k.Name) + "/" + escapeKeyPart(k.Scope)
}

var keyEscaper = strings.NewReplacer("%", "%25", "/", "%2F")

func escapeKeyPart(s string) string { return keyEscaper.Replace(s) }

func (k Key) validate() error {
	if k.Name == "" {
		return fmt.Errorf("%w: key name must not be empty", ErrInvalidConfig)
	}
	if len(k.Name) > maxNameLen {
		return fmt.Errorf("%w: key name must be at most %d bytes, got %d", ErrInvalidConfig, maxNameLen, len(k.Name))
	}
	if !utf8.ValidString(k.Name) {
		return fmt.Errorf("%w: key name must be valid UTF-8", ErrInvalidConfig)
	}
	if len(k.Scope) > maxScopeLen {
		return fmt.Errorf("%w: key scope must be at most %d bytes, got %d", ErrInvalidConfig, maxScopeLen, len(k.Scope))
	}
	if !utf8.ValidString(k.Scope) {
		return fmt.Errorf("%w: key scope must be valid UTF-8", ErrInvalidConfig)
	}
	return nil
}
