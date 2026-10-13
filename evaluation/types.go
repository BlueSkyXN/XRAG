// Package evaluation scores explicit human expectations. It never invents
// ground truth from model answers and keeps empty-result cases separate.
package evaluation

import (
	"strings"
	"github.com/BlueSkyXN/XRAG/core"
)

type Expected struct {
	Path   string `json:"path"`
	Anchor string `json:"anchor,omitempty"`
}
type Case struct {
	ID            string     `json:"id"`
	Question      string     `json:"question"`
	Expected      []Expected `json:"expected"`
	ExpectedEmpty bool       `json:"expected_empty"`
}
type Set struct {
	Family   string `json:"family,omitempty"`
	Purpose  string `json:"purpose,omitempty"`
	ID       string `json:"id,omitempty"`
	KB       string `json:"kb"`
	Name     string `json:"name"`
	Revision string `json:"revision,omitempty"`
	Cases    []Case `json:"cases"`
}
type Request struct {
	Set        Set      `json:"set"`
	Strategies []string `json:"strategies"`
	K          int      `json:"k"`
}
type Score struct {
	Hit            float64  `json:"hit"`
	Recall         float64  `json:"recall"`
	ReciprocalRank float64  `json:"reciprocal_rank"`
	EmptyCorrect   bool     `json:"empty_correct"`
	Matched        []string `json:"matched_paths"`
	Missed         []string `json:"missed_paths"`
}
type Row struct {
	CaseID        string         `json:"case_id"`
	Question      string         `json:"question"`
	ExpectedEmpty bool           `json:"expected_empty"`
	Strategy      string         `json:"strategy"`
	QueryID       string         `json:"query_id"`
	Score         Score          `json:"score"`
	DurationMS    int64          `json:"duration_ms"`
	Items         []core.Item    `json:"items"`
	Trace         []core.Step    `json:"trace"`
	Error         *core.Error    `json:"error,omitempty"`
	Actual        map[string]any `json:"actual_strategy"`
}
type Summary struct {
	Strategy         string   `json:"strategy"`
	Cases            int      `json:"cases"`
	Positives        int      `json:"positives"`
	Negatives        int      `json:"negatives"`
	Errors           int      `json:"errors"`
	HitAtK           float64  `json:"hit_at_k"`
	RecallAtK        float64  `json:"recall_at_k"`
	MRR              float64  `json:"mrr"`
	NegativeAccuracy *float64 `json:"negative_accuracy"`
	P50MS            int64    `json:"p50_ms"`
	P95MS            int64    `json:"p95_ms"`
}
type Pair struct {
	A     string   `json:"a"`
	B     string   `json:"b"`
	OnlyA []string `json:"only_a"`
	OnlyB []string `json:"only_b"`
}
type Report struct {
	Family       string    `json:"family,omitempty"`
	Purpose      string    `json:"purpose,omitempty"`
	SetID        string    `json:"set_id"`
	SetRevision  string    `json:"set_revision"`
	KB           string    `json:"kb"`
	K            int       `json:"k"`
	ViewEpoch    int64     `json:"view_epoch"`
	ManifestHash string    `json:"manifest_hash"`
	SettingsHash string    `json:"settings_hash"`
	Rows         []Row     `json:"rows"`
	Summaries    []Summary `json:"summaries"`
	Pairs        []Pair    `json:"pairs"`
	Notes        []string  `json:"notes"`
}

var Strategies = []string{"keyword", "semantic", "ask", "ask_graph", "ask_rerank"}

func ValidateSet(s Set) error {
	if s.Family != "" && !core.ValidKB(s.Family) {
		return core.Err("INVALID_ARGUMENT", "family must be a short slug", 400)
	}
	if s.Purpose != "" && s.Purpose != "development" && s.Purpose != "holdout" {
		return core.Err("INVALID_ARGUMENT", "purpose is development or holdout", 400)
	}
	questions := map[string]bool{}
	for _, c := range s.Cases {
		q := strings.ToLower(strings.Join(strings.Fields(c.Question), " "))
		if questions[q] {
			return core.Err("EVAL_DUPLICATE_QUESTION", "duplicate normalized question within dataset", 400)
		}
		questions[q] = true
	}

	if !core.ValidKB(s.KB) || strings.TrimSpace(s.Name) == "" || len(s.Name) > 160 || len(s.Cases) == 0 || len(s.Cases) > 100 {
		return core.Err("INVALID_ARGUMENT", "evaluation needs KB, name and 1..100 cases", 400)
	}
	ids := map[string]bool{}
	for _, c := range s.Cases {
		if !core.ValidKB(c.ID) || ids[c.ID] || strings.TrimSpace(c.Question) == "" || len(c.Question) > 2000 {
			return core.Err("INVALID_ARGUMENT", "each case needs a distinct short id and question", 400)
		}
		ids[c.ID] = true
		if c.ExpectedEmpty {
			if len(c.Expected) > 0 {
				return core.Err("INVALID_ARGUMENT", "expected_empty and expected targets cannot both be set", 400)
			}
		} else if len(c.Expected) == 0 || len(c.Expected) > 20 {
			return core.Err("INVALID_ARGUMENT", "positive cases require 1..20 explicit expected targets", 400)
		}
		targets := map[string]bool{}
		for _, x := range c.Expected {
			if _, e := core.CleanPath(x.Path); e != nil {
				return e
			}
			key := core.JSON(x)
			if targets[key] || len(x.Anchor) > 2000 {
				return core.Err("INVALID_ARGUMENT", "duplicate or oversized expected target", 400)
			}
			targets[key] = true
		}
	}
	return nil
}
func ScoreCase(c Case, items []core.Item, k int) Score {
	s := Score{Matched: []string{}, Missed: []string{}}
	if k < len(items) {
		items = items[:k]
	}
	if c.ExpectedEmpty {
		s.EmptyCorrect = len(items) == 0
		return s
	}
	first := 0
	for _, x := range c.Expected {
		rank := 0
		for i, it := range items {
			if it.Path == x.Path && (x.Anchor == "" || strings.Contains(it.Text, x.Anchor)) {
				rank = i + 1
				break
			}
		}
		if rank > 0 {
			s.Matched = append(s.Matched, x.Path)
			if first == 0 || rank < first {
				first = rank
			}
		} else {
			s.Missed = append(s.Missed, x.Path)
		}
	}
	if first > 0 {
		s.Hit = 1
		s.ReciprocalRank = 1 / float64(first)
	}
	s.Recall = float64(len(s.Matched)) / float64(len(c.Expected))
	return s
}
