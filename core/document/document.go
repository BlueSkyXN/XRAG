package document

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const (
	maxRunes = 1200
	minRunes = 150
)

type Field struct {
	Name   string
	Type   string
	Key    string
	Text   string
	Number *float64
}

type Chunk struct {
	Heading   []string
	Text      string
	CharStart int
	CharEnd   int
	LineStart int
	LineEnd   int
	Dup       int
	Key       string
}

type Parsed struct {
	Content string
	Fields  []Field
	Chunks  []Chunk
}

func Normalize(raw []byte) (string, error) {
	if bytes.HasPrefix(raw, []byte{0xEF, 0xBB, 0xBF}) {
		raw = raw[3:]
	}
	if !utf8.Valid(raw) {
		return "", errDecode
	}
	s := strings.ReplaceAll(string(raw), "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return norm.NFC.String(s), nil
}

var errDecode = decodeError{}

type decodeError struct{}

func (decodeError) Error() string { return "file is not valid UTF-8" }

func Parse(raw []byte) (Parsed, error) {
	content, err := Normalize(raw)
	if err != nil {
		return Parsed{}, err
	}
	runes := []rune(content)
	body := 0
	var fields []Field
	if strings.HasPrefix(content, "---\n") {
		rest := string(runes[4:])
		if idx := strings.Index(rest, "\n---\n"); idx >= 0 {
			fields = frontmatter(rest[:idx])
			body = 4 + utf8.RuneCountInString(rest[:idx]) + len([]rune("\n---\n"))
		}
	}
	chunks := sections(runes, body)
	return Parsed{Content: content, Fields: fields, Chunks: chunks}, nil
}

func frontmatter(block string) []Field {
	var out []Field
	for _, line := range strings.Split(block, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || strings.ContainsAny(key, " \t#") {
			continue
		}
		value = strings.Trim(value, `"'`)
		f := Field{Name: key, Type: "text", Text: value}
		if n, err := strconv.ParseFloat(value, 64); err == nil && strconv.FormatFloat(n, 'f', -1, 64) == value {
			f.Type = "number"
			f.Number = &n
			f.Text = ""
		}
		raw, _ := json.Marshal([]any{f.Type, value})
		f.Key = string(raw)
		out = append(out, f)
	}
	return out
}

type section struct {
	heading []string
	start   int
	end     int
}

func sections(runes []rune, body int) []Chunk {
	var lines []struct{ start, end int }
	for i := body; i <= len(runes); i++ {
		if i == len(runes) || runes[i] == '\n' {
			lines = append(lines, struct{ start, end int }{body, i})
			body = i + 1
			if i == len(runes) {
				break
			}
		}
	}
	var stack []string
	var secs []section
	flush := func(start, end int, heading []string) {
		if end < start {
			return
		}
		secs = append(secs, section{heading: append([]string{}, heading...), start: start, end: end})
	}
	curStart := -1
	curHeading := []string{}
	for _, ln := range lines {
		text := strings.TrimRight(string(runes[ln.start:ln.end]), " \t")
		if depth, title, ok := heading(text); ok {
			if curStart >= 0 {
				flush(curStart, ln.start, curHeading)
			}
			if depth <= len(stack) {
				stack = stack[:depth-1]
			}
			stack = append(stack, title)
			curHeading = append([]string{}, stack...)
			curStart = ln.start
			continue
		}
		if curStart < 0 {
			curStart = ln.start
			curHeading = nil
		}
	}
	if curStart >= 0 {
		flush(curStart, len(runes), curHeading)
	}
	secs = mergeShort(runes, secs)
	var out []Chunk
	seen := map[string]int{}
	for _, sec := range secs {
		for _, piece := range windows(runes, sec) {
			text := string(runes[piece.start:piece.end])
			if strings.TrimSpace(text) == "" {
				continue
			}
			sig := strings.Join(piece.heading, "\x1f") + "\n" + text
			dup := seen[sig]
			seen[sig] = dup + 1
			out = append(out, Chunk{
				Heading:   piece.heading,
				Text:      text,
				CharStart: piece.start,
				CharEnd:   piece.end,
				LineStart: 1 + strings.Count(string(runes[:piece.start]), "\n"),
				LineEnd:   1 + strings.Count(string(runes[:piece.end]), "\n"),
				Dup:       dup,
			})
		}
	}
	return out
}

func heading(text string) (int, string, bool) {
	i := 0
	for i < len(text) && text[i] == '#' {
		i++
	}
	if i < 1 || i > 3 || i >= len(text) || (text[i] != ' ' && text[i] != '\t') {
		return 0, "", false
	}
	title := strings.TrimSpace(text[i:])
	if title == "" {
		return 0, "", false
	}
	return i, title, true
}

func mergeShort(runes []rune, secs []section) []section {
	if len(secs) == 0 {
		return secs
	}
	var out []section
	cur := secs[0]
	for _, next := range secs[1:] {
		span := cur.end - cur.start
		if span < minRunes && next.end-cur.start <= maxRunes {
			cur.end = next.end
			continue
		}
		out = append(out, cur)
		cur = next
	}
	out = append(out, cur)
	return out
}

func windows(runes []rune, sec section) []section {
	if sec.end-sec.start <= maxRunes {
		return []section{sec}
	}
	var out []section
	start := sec.start
	for start < sec.end {
		end := start + maxRunes
		if end > sec.end {
			end = sec.end
		} else {
			for end > start && runes[end-1] != '\n' {
				end--
			}
			if end <= start {
				end = start + maxRunes
			}
		}
		out = append(out, section{heading: sec.heading, start: start, end: end})
		start = end
	}
	return out
}
