package sequence

import (
	"math/rand"
	"strconv"
	"testing"
)

func TestLuhnKnownVector(t *testing.T) {
	// Textbook example: 7992739871 + check digit 3.
	if got := luhnCheckDigit("7992739871"); got != '3' {
		t.Fatalf("check digit = %c, want 3", got)
	}
	if !ValidLuhn("79927398713") || ValidLuhn("79927398710") {
		t.Fatal("ValidLuhn disagrees with the textbook vector")
	}
}

func TestValidLuhnEdgeCases(t *testing.T) {
	for _, s := range []string{"", "0", "7", "abc"} {
		if ValidLuhn(s) {
			t.Errorf("%q must be invalid (fewer than 2 digits)", s)
		}
	}
	if !ValidLuhn("INV-7992-7398-713") {
		t.Error("non-digits must be ignored")
	}
	if !ValidLuhn("00") {
		t.Error(`"00" is a valid payload and check digit`)
	}
}

func randDigits(r *rand.Rand, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte('0' + r.Intn(10))
	}
	return string(b)
}

// Luhn catches every single-digit error and every adjacent transposition except 09 <-> 90.
func TestLuhnErrorDetection(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for range 500 {
		payload := randDigits(r, 4+r.Intn(14))
		full := payload + string(luhnCheckDigit(payload))
		if !ValidLuhn(full) {
			t.Fatalf("%s must validate", full)
		}
		for i := range full {
			for d := byte('0'); d <= '9'; d++ {
				if d == full[i] {
					continue
				}
				mut := full[:i] + string(d) + full[i+1:]
				if ValidLuhn(mut) {
					t.Fatalf("single-digit error %s -> %s accepted", full, mut)
				}
			}
		}
		for i := 0; i+1 < len(full); i++ {
			a, b := full[i], full[i+1]
			if a == b {
				continue
			}
			mut := full[:i] + string(b) + string(a) + full[i+2:]
			undetectable := (a == '0' && b == '9') || (a == '9' && b == '0')
			if ValidLuhn(mut) != undetectable {
				t.Fatalf("transposition %s -> %s: accepted=%v, want %v", full, mut, ValidLuhn(mut), undetectable)
			}
		}
	}
}

func TestLuhnRoundTripIncludesSeq(t *testing.T) {
	for seq := int64(1); seq < 2000; seq++ {
		s := "INV-2026-" + strconv.FormatInt(seq, 10)
		if !ValidLuhn(s + string(luhnCheckDigit(s))) {
			t.Fatalf("round trip failed for %s", s)
		}
	}
}

func FuzzValidLuhn(f *testing.F) {
	for _, s := range []string{"", "79927398713", "a1b2", "\xff\x00"} {
		f.Add(s)
	}
	f.Fuzz(func(_ *testing.T, s string) { ValidLuhn(s) })
}
