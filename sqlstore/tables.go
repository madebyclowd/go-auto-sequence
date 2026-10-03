package sqlstore

import (
	"fmt"
	"regexp"

	sequence "github.com/madebyclowd/go-auto-sequence"
)

const defaultTable = "sequences"

var tableName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)?$`)

// validateTable accepts a bare or schema-qualified identifier. Identifiers are interpolated
// into the prebuilt statements unquoted, so anything else is rejected; values are always
// bound parameters.
func validateTable(name string) error {
	if len(name) > 128 || !tableName.MatchString(name) {
		return fmt.Errorf("%w: invalid table name %q", sequence.ErrInvalidConfig, name)
	}
	return nil
}
