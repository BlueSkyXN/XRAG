package core

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const ParserRevision = "demo-lines-2" // deliberately not a full CommonMark/YAML implementation
var slugRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

func ValidKB(s string) bool { return slugRE.MatchString(s) }
func CleanPath(s string) (string, error) {
	s = strings.ReplaceAll(s, "\\", "/")
	if s == "" || strings.HasPrefix(s, "/") || strings.Contains(s, "\x00") {
		return "", Err("INVALID_ARGUMENT", "invalid relative path", 400)
	}
	for _, p := range strings.Split(s, "/") {
		if p == "" || p == ".." || p == "." {
			return "", Err("INVALID_ARGUMENT", "path contains invalid component", 400)
		}
	}
	if path.Clean(s) != s {
		return "", Err("INVALID_ARGUMENT", "path is not canonical", 400)
	}
	return s, nil
}
func Normalize(b []byte) (string, error) {
	if !utf8.Valid(b) {
		return "", Err("INVALID_ENCODING", "UTF-8 is required", 400)
	}
	s := strings.TrimPrefix(string(b), "\ufeff")
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n"), nil
}
func Parse(kb, p, body, edition, source string) (Document, error) {
	return ParseWithLimit(kb, p, body, edition, source, 1200)
}
func ParseWithLimit(kb, p, body, edition, source string, maxChars int) (Document, error) {
	if maxChars == 0 {
		maxChars = 1200
	}
	if maxChars < 100 || maxChars > 20000 {
		return Document{}, Err("INVALID_ARGUMENT", "chunk size outside bounds", 400)
	}
	d := Document{KB: kb, Path: p, Body: body, Edition: edition, Source: source, FileHash: TextHash(body), Title: path.Base(p), Fields: map[string]any{}, Chunks: []Chunk{}, Supports: []Support{}, Kind: "doc"}
	if !ValidKB(kb) {
		return d, Err("INVALID_ARGUMENT", "invalid knowledge base id", 400)
	}
	if _, e := CleanPath(p); e != nil {
		return d, e
	}
	lines := strings.SplitAfter(body, "\n")
	startLine := 0
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		end := -1
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				end = i
				break
			}
		}
		if end < 0 {
			return d, Err("INVALID_FRONTMATTER", "unterminated frontmatter", 400)
		}
		for _, line := range lines[1:end] {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			k, v, ok := strings.Cut(line, ":")
			k = strings.TrimSpace(k)
			v = strings.TrimSpace(v)
			if !ok || k == "" || v == "" {
				return d, Err("INVALID_FRONTMATTER", "front matter accepts scalar keys and single-line lists only", 400)
			}
			var value any
			if json.Unmarshal([]byte(v), &value) != nil {
				if strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
					ss := strings.Split(strings.Trim(v, "[]"), ",")
					a := []any{}
					for _, s := range ss {
						a = append(a, strings.Trim(strings.TrimSpace(s), "\"'"))
					}
					value = a
				} else if n, e := strconv.ParseFloat(v, 64); e == nil {
					value = n
				} else {
					value = strings.Trim(v, "\"'")
				}
			}
			d.Fields[k] = value
		}
		startLine = end + 1
		if t, ok := d.Fields["title"].(string); ok {
			d.Title = t
		}
	}
	heading := []string{}
	starts := make([]int, len(lines)+1)
	for i, l := range lines {
		starts[i+1] = starts[i] + utf8.RuneCountInString(l)
	}
	runes := []rune(body)
	dup := map[string]int{}
	blockStart := startLine
	fence := false
	add := func(a, b int) {
		if a >= b {
			return
		}
		lo, hi := starts[a], starts[b]
		for lo < hi {
			end := lo + maxChars
			if end > hi {
				end = hi
			}
			text := string(runes[lo:end])
			if strings.TrimSpace(text) != "" {
				keybase := Hash([]any{2, kb, p, heading, text})
				idx := dup[keybase]
				dup[keybase]++
				key := "k" + Hash([]any{2, kb, p, heading, text, idx})[:32]
				ls := strings.Count(string(runes[:lo]), "\n") + 1
				le := ls + strings.Count(strings.TrimSuffix(text, "\n"), "\n")
				d.Chunks = append(d.Chunks, Chunk{ID: UUID([]any{edition, key}), Key: key, KB: kb, Path: p, Edition: edition, FileHash: d.FileHash, Title: d.Title, Heading: append([]string{}, heading...), Text: text, Start: lo, End: end, LineStart: ls, LineEnd: le, Fields: d.Fields})
			}
			lo = end
		}
	}
	for i := startLine; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			fence = !fence
		}
		if fence {
			continue
		}
		level := 0
		for level < len(t) && t[level] == '#' {
			level++
		}
		if level >= 1 && level <= 3 && len(t) > level && t[level] == ' ' {
			if i > blockStart {
				add(blockStart, i)
			}
			if len(heading) >= level {
				heading = heading[:level-1]
			}
			heading = append(heading, strings.TrimSpace(t[level:]))
			if level == 1 {
				if _, ok := d.Fields["title"]; !ok {
					d.Title = heading[len(heading)-1]
				}
			}
			blockStart = i
		}
	}
	add(blockStart, len(lines))
	for i := range d.Chunks {
		d.Chunks[i].Title = d.Title
	}
	return d, nil
}

// Terms uses an explicitly documented bigram fallback, not gse or model semantics.
func Terms(s string, query bool) []string {
	out := []string{}
	r := []rune(strings.ToLower(s))
	for i := 0; i < len(r); {
		if unicode.Is(unicode.Han, r[i]) {
			j := i
			for j < len(r) && unicode.Is(unicode.Han, r[j]) {
				j++
			}
			if !query || j-i == 1 {
				for k := i; k < j; k++ {
					out = append(out, string(r[k]))
				}
			}
			for k := i; k+1 < j; k++ {
				out = append(out, string(r[k:k+2]))
			}
			i = j
		} else if unicode.IsLetter(r[i]) || unicode.IsDigit(r[i]) {
			j := i + 1
			for j < len(r) && (unicode.IsLetter(r[j]) && !unicode.Is(unicode.Han, r[j]) || unicode.IsDigit(r[j]) || r[j] == '_' || r[j] == '-' || r[j] == '.') {
				j++
			}
			out = append(out, string(r[i:j]))
			i = j
		} else {
			i++
		}
	}
	return out
}
func FTSQuery(q string) (string, error) {
	terms := Unique(Terms(q, true))
	if len(terms) == 0 || len(terms) > 48 {
		return "", Err("INVALID_ARGUMENT", "query needs 1..48 terms", 400)
	}
	quoted := []string{}
	for _, s := range terms {
		quoted = append(quoted, "\""+strings.ReplaceAll(s, "\"", "\"\"")+"\"")
	}
	return strings.Join(quoted, " AND "), nil
}

var relationRE = regexp.MustCompile(`\[\[([^\]\n]+)\]\]\s*--([^\n>]+)-->\s*\[\[([^\]\n]+)\]\]`)
var mentionRE = regexp.MustCompile(`\[\[([^\]\n]+)\]\]`)

func ExplicitSupports(d Document) []Support {
	out := []Support{}
	seen := map[string]bool{}
	for _, c := range d.Chunks {
		for _, m := range relationRE.FindAllStringSubmatch(c.Text, -1) {
			a, rel, b := strings.TrimSpace(m[1]), strings.TrimSpace(m[2]), strings.TrimSpace(m[3])
			id := "sp_" + Hash([]any{c.ID, "explicit-v1", a, rel, b})[:32]
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, Support{ID: id, KB: c.KB, Path: c.Path, Edition: c.Edition, ChunkID: c.ID, From: a, To: b, Relation: rel, Evidence: m[0], Origin: "explicit"})
		}
	}
	return out
}
func Mentions(c Chunk) []string {
	if c.MentionNames != nil {
		return c.MentionNames
	}
	xs := []string{}
	for _, m := range mentionRE.FindAllStringSubmatch(c.Text, -1) {
		xs = append(xs, strings.TrimSpace(m[1]))
	}
	return Unique(xs)
}
func EntityID(kb, name string) string {
	return "en_" + Hash([]string{kb, strings.ToLower(strings.TrimSpace(name))})[:24]
}
func PathMatch(s, prefix string) bool {
	return prefix == "" || s == strings.TrimSuffix(prefix, "/") || strings.HasPrefix(s, strings.TrimSuffix(prefix, "/")+"/")
}
func MatchFilter(c Chunk, f Filter) (bool, error) {
	var actual any
	switch f.Field {
	case "path":
		actual = c.Path
	case "kb":
		actual = c.KB
	case "title":
		actual = c.Title
	default:
		if !strings.HasPrefix(f.Field, "fields.") {
			return false, Err("FILTER_UNSUPPORTED", "field must be path/kb/title or fields.<name>", 422)
		}
		actual = c.Fields[strings.TrimPrefix(f.Field, "fields.")]
	}
	switch f.Op {
	case "eq", "in":
		vals, ok := actual.([]any)
		if !ok {
			vals = []any{actual}
		}
		want, ok := f.Value.([]any)
		if !ok {
			want = []any{f.Value}
		}
		for _, a := range vals {
			for _, b := range want {
				if a != nil && JSON(a) == JSON(b) {
					return true, nil
				}
			}
		}
		return false, nil
	case "prefix":
		if f.Field != "path" {
			return false, Err("FILTER_UNSUPPORTED", "prefix is only available for path", 422)
		}
		return PathMatch(c.Path, fmt.Sprint(f.Value)), nil
	default:
		return false, Err("FILTER_UNSUPPORTED", "supported filter ops: eq, in, prefix", 422)
	}
}

// Dictionary terms are exact domain tokens, not a model or an approximate semantic matcher.
func DictionaryTerms(text string, dictionary []string, query bool) []string {
	if len(dictionary) == 0 {
		return Terms(text, query)
	}
	words := Unique(dictionary)
	sort.Slice(words, func(i, j int) bool { return len([]rune(words[i])) > len([]rune(words[j])) })
	lower := strings.ToLower(text)
	if !query {
		out := Terms(text, false)
		for _, w := range words {
			if strings.Contains(lower, strings.ToLower(w)) {
				out = append(out, "domain"+Hash(strings.ToLower(w))[:24])
			}
		}
		return out
	}
	matched := []string{}
	for _, w := range words {
		w = strings.ToLower(w)
		if strings.Contains(lower, w) {
			matched = append(matched, "domain"+Hash(w)[:24])
			lower = strings.ReplaceAll(lower, w, " ")
		}
	}
	return append(matched, Terms(lower, true)...)
}
func DictionaryQuery(q string, words []string) (string, error) {
	ts := Unique(DictionaryTerms(q, words, true))
	if len(ts) == 0 || len(ts) > 48 {
		return "", Err("INVALID_ARGUMENT", "query needs 1..48 terms", 400)
	}
	quoted := []string{}
	for _, t := range ts {
		quoted = append(quoted, "\""+strings.ReplaceAll(t, "\"", "\"\"")+"\"")
	}
	return strings.Join(quoted, " AND "), nil
}
