package sequence

// luhnCheckDigit returns the Luhn check digit for the ASCII digits in payload; every other
// byte is ignored. The rightmost payload digit is doubled first, because the check digit
// that will follow it takes the undoubled position.
func luhnCheckDigit(payload string) byte {
	sum, double := 0, true
	for i := len(payload) - 1; i >= 0; i-- {
		c := payload[i]
		if c < '0' || c > '9' {
			continue
		}
		d := int(c - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return byte('0' + (10-sum%10)%10)
}

// ValidLuhn reports whether the digits of s, ignoring every other character, end in a
// correct Luhn check digit. Fewer than two digits is never valid.
//
// Luhn detects every single-digit error and most adjacent transpositions, but it cannot
// detect the swap of 09 and 90. It guards against typos, not tampering.
func ValidLuhn(s string) bool {
	digits := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			digits = append(digits, s[i])
		}
	}
	if len(digits) < 2 {
		return false
	}
	payload := digits[:len(digits)-1]
	return luhnCheckDigit(string(payload)) == digits[len(digits)-1]
}
