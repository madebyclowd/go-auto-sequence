package sequence

// Number is an issued document number.
type Number struct {
	Value  string // formatted, e.g. "INV-2026-00042"
	Seq    int64  // visible sequence number
	Period string // stored period key, e.g. "2026-10"; empty for Never
	Key    Key
}

// String returns the formatted value, so a Number prints as the document number.
func (n Number) String() string { return n.Value }
