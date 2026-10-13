package core

import (
	"path"
	"regexp"
	"strings"
)

func ValidateGlob(p string) error {
	if strings.ContainsAny(p, "[]{}") {
		return Err("INVALID_ARGUMENT", "glob supports *, ** and ? only", 400)
	}
	if p == "" || strings.HasPrefix(p, "/") || strings.Contains(p, "\\") || strings.Contains(p, "..") || len(p) > 500 {
		return Err("INVALID_ARGUMENT", "glob must be a relative pattern without ..", 400)
	}
	_, e := path.Match(strings.ReplaceAll(p, "**", "*"), "test")
	if e != nil {
		return Err("INVALID_ARGUMENT", "invalid glob", 400)
	}
	return nil
}
func GlobMatch(pattern, s string) bool {
	if strings.HasSuffix(pattern, "/") {
		return PathMatch(s, pattern)
	}
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
					b.WriteString("(?:.*/)?")
				} else {
					b.WriteString(".*")
				}
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		default:
			b.WriteString(regexp.QuoteMeta(string(pattern[i])))
		}
	}
	b.WriteString("$")
	r, e := regexp.Compile(b.String())
	return e == nil && r.MatchString(s)
}
func Included(p string, include, exclude []string) bool {
	yes := len(include) == 0
	for _, g := range include {
		if GlobMatch(g, p) {
			yes = true
		}
	}
	for _, g := range exclude {
		if GlobMatch(g, p) {
			return false
		}
	}
	return yes
}
