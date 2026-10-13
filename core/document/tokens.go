package document

import (
	"strings"
	"unicode"
)

// Tokens produces the M1 lexical tokens. ASCII identifiers stay whole, so E203
// is not a prefix of E2031. Han text also emits bigrams because unicode61 does
// not segment Chinese; this is not a validated gse dictionary.
func Tokens(text string) []string {
	runes := []rune(text)
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		if s == "" || seen[s] {
			return
		}
		seen[s] = true
		out = append(out, s)
	}
	for i := 0; i < len(runes); {
		r := runes[i]
		switch {
		case unicode.Is(unicode.Han, r):
			j := i + 1
			for j < len(runes) && unicode.Is(unicode.Han, runes[j]) {
				j++
			}
			add(string(runes[i:j]))
			for k := i; k+1 < j; k++ {
				add(string(runes[k : k+2]))
			}
			i = j
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			j := i + 1
			for j < len(runes) && (unicode.IsLetter(runes[j]) || unicode.IsDigit(runes[j])) && !unicode.Is(unicode.Han, runes[j]) {
				j++
			}
			add(string(runes[i:j]))
			i = j
		default:
			i++
		}
	}
	return out
}

func IndexText(text string) string { return strings.Join(Tokens(text), " ") }

func MatchQuery(text string) string {
	parts := Tokens(text)
	quoted := make([]string, len(parts))
	for i, part := range parts {
		quoted[i] = `"` + strings.ReplaceAll(part, `"`, `""`) + `"`
	}
	return strings.Join(quoted, " ")
}

func Accept(text, query string) bool {
	q := strings.TrimSpace(query)
	if q == "" {
		return false
	}
	if ident(q) {
		return bounded(text, q)
	}
	return strings.Contains(text, q)
}

func ident(s string) bool {
	if s == "" || !isIdent(s[0]) || s[0] >= '0' && s[0] <= '9' {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isIdent(s[i]) {
			return false
		}
	}
	return true
}

func isIdent(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z'
}

func bounded(text, ident string) bool {
	for from := 0; from < len(text); {
		i := strings.Index(text[from:], ident)
		if i < 0 {
			return false
		}
		i += from
		before := i == 0 || !isIdent(text[i-1])
		afterAt := i + len(ident)
		after := afterAt >= len(text) || !isIdent(text[afterAt])
		if before && after {
			return true
		}
		from = i + 1
	}
	return false
}
