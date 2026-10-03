package sequence

import "testing"

func TestNumber(t *testing.T) {
	_ = map[Number]struct{}{} // Number must stay comparable
	if got := (Number{Value: "INV-1"}).String(); got != "INV-1" {
		t.Fatalf("String() = %q", got)
	}
}
