package sequence

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Parts is everything a Formatter may use to render one number.
type Parts struct {
	Key    Key
	Period string
	Seq    int64
	At     time.Time         // issue time, already converted to the configured location
	Vars   map[string]string // from the Var and Vars call options
}

// Formatter renders a number. *Format implements it; use FormatFunc for a custom rule.
type Formatter interface {
	Format(Parts) (string, error)
}

// FormatFunc adapts an ordinary function to a Formatter, like http.HandlerFunc.
type FormatFunc func(Parts) (string, error)

// Format calls f(p).
func (f FormatFunc) Format(p Parts) (string, error) { return f(p) }

const maxSeqWidth = 32

type segKind uint8

const (
	segLiteral segKind = iota
	segSeq
	segName
	segScope
	segPeriod
	segDate
	segVar
	segCheck
)

type segment struct {
	kind  segKind
	text  string // literal text, date layout, or variable key
	width int    // minimum width of a sequence value
}

// dateTokens maps date tokens to Go reference layouts.
var dateTokens = map[string]string{
	"YYYY": "2006", "YY": "06", "MM": "01", "M": "1", "DD": "02", "D": "2",
	"HH": "15", "mm": "04", "ss": "05",
}

// Format is a compiled template. It is immutable and safe for concurrent use.
//
// Tokens: {seq}, {seq:N} (minimum zero-padded width), {name}, {scope}, {period}, the date
// tokens {YYYY} {YY} {MM} {M} {DD} {D} {HH} {mm} {ss}, {var:key}, and {check:luhn} (which must
// be the last token). {{ and }} are literal braces.
type Format struct {
	src  string
	segs []segment
}

// ParseFormat compiles template. Every error wraps ErrInvalidFormat and names the byte offset.
func ParseFormat(template string) (*Format, error) {
	f := &Format{src: template}
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			f.segs = append(f.segs, segment{kind: segLiteral, text: lit.String()})
			lit.Reset()
		}
	}
	fail := func(off int, format string, a ...any) (*Format, error) {
		return nil, fmt.Errorf("%w: offset %d: %s", ErrInvalidFormat, off, fmt.Sprintf(format, a...))
	}
	hasSeq, hasCheck := false, false
	for i := 0; i < len(template); {
		c := template[i]
		if hasCheck {
			return fail(i, "{check:luhn} must be the last token; nothing may follow it")
		}
		switch {
		case c == '{' && i+1 < len(template) && template[i+1] == '{':
			lit.WriteByte('{')
			i += 2
		case c == '}' && i+1 < len(template) && template[i+1] == '}':
			lit.WriteByte('}')
			i += 2
		case c == '}':
			return fail(i, "unmatched '}' (write }} for a literal brace)")
		case c == '{':
			end := strings.IndexByte(template[i:], '}')
			if end < 0 {
				return fail(i, "unterminated '{'")
			}
			tok := template[i+1 : i+end]
			seg, err := parseToken(tok)
			if err != nil {
				return fail(i, "%v", err)
			}
			flush()
			f.segs = append(f.segs, seg)
			hasSeq = hasSeq || seg.kind == segSeq
			hasCheck = seg.kind == segCheck
			i += end + 1
		default:
			lit.WriteByte(c)
			i++
		}
	}
	flush()
	if !hasSeq {
		return fail(len(template), "template needs {seq} or {seq:N}, or numbers cannot be unique")
	}
	return f, nil
}

func parseToken(tok string) (segment, error) {
	name, arg, hasArg := strings.Cut(tok, ":")
	if name == "" {
		return segment{}, fmt.Errorf("empty token")
	}
	if layout, ok := dateTokens[name]; ok && !hasArg {
		return segment{kind: segDate, text: layout}, nil
	}
	switch name {
	case "seq":
		if !hasArg {
			return segment{kind: segSeq}, nil
		}
		n, err := strconv.Atoi(arg)
		if err != nil || n < 1 || n > maxSeqWidth {
			return segment{}, fmt.Errorf("{seq:%s}: width must be an integer 1..%d", arg, maxSeqWidth)
		}
		return segment{kind: segSeq, width: n}, nil
	case "name", "scope", "period":
		if hasArg {
			return segment{}, fmt.Errorf("{%s} takes no argument", name)
		}
		return segment{kind: map[string]segKind{"name": segName, "scope": segScope, "period": segPeriod}[name]}, nil
	case "var":
		if !hasArg || !validVarKey(arg) {
			return segment{}, fmt.Errorf("{var:KEY}: KEY must match [A-Za-z_][A-Za-z0-9_]*")
		}
		return segment{kind: segVar, text: arg}, nil
	case "check":
		if arg != "luhn" {
			return segment{}, fmt.Errorf("{check:%s}: only {check:luhn} is supported", arg)
		}
		return segment{kind: segCheck}, nil
	}
	return segment{}, fmt.Errorf("unknown token {%s}", tok)
}

func validVarKey(k string) bool {
	if k == "" {
		return false
	}
	for i := 0; i < len(k); i++ {
		c := k[i]
		switch {
		case c == '_', c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}

// Format renders one number. It returns ErrMissingVar when a {var:key} has no value in p.Vars.
func (f *Format) Format(p Parts) (string, error) {
	var b strings.Builder
	b.Grow(len(f.src) + 16)
	var scratch [24]byte
	for _, s := range f.segs {
		switch s.kind {
		case segLiteral:
			b.WriteString(s.text)
		case segSeq:
			digits := strconv.AppendInt(scratch[:0], p.Seq, 10)
			for n := len(digits); n < s.width; n++ {
				b.WriteByte('0')
			}
			b.Write(digits)
		case segName:
			b.WriteString(p.Key.Name)
		case segScope:
			b.WriteString(p.Key.Scope)
		case segPeriod:
			b.WriteString(p.Period)
		case segDate:
			b.Write(p.At.AppendFormat(scratch[:0], s.text))
		case segVar:
			v, ok := p.Vars[s.text]
			if !ok {
				return "", fmt.Errorf("%w: %q", ErrMissingVar, s.text)
			}
			b.WriteString(v)
		case segCheck:
			b.WriteByte(luhnCheckDigit(b.String()))
		}
	}
	return b.String(), nil
}

// String returns the original template.
func (f *Format) String() string { return f.src }
