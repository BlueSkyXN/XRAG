package core

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestLexicalSparseGolden(t *testing.T) {
	v := LexicalSparse("hello hello", false)
	if !reflect.DeepEqual(v.Indices, []uint32{0x4f9f2cab}) || len(v.Values) != 1 || math.Abs(v.Values[0]-1.88969972484747) > 1e-12 {
		t.Fatalf("FNV-1a / BM25: %+v", v)
	}
	if LexicalSchema != "xrag-terms-fnv1a32-bm25-v1" || LexicalAverageLength != 3231.0/51.0 {
		t.Fatal("schema constants changed")
	}
}

func TestLexicalSparseCollision(t *testing.T) {
	// 两个不同英文词的 FNV-1a 32 位哈希相同，先合并词频再做 BM25。
	v := LexicalSparse("costarring liquid", false)
	want := LexicalSparse("hello hello", false)
	if !reflect.DeepEqual(v.Indices, []uint32{0x5e4daa9d}) || !reflect.DeepEqual(v.Values, want.Values) {
		t.Fatal(v, want)
	}
	q := LexicalSparse("costarring liquid costarring", true)
	if !reflect.DeepEqual(q.Values, []float64{2}) {
		t.Fatal(q)
	}
}

func TestLexicalSparseQueryTerms(t *testing.T) {
	if !reflect.DeepEqual(LexicalSparse("如何排查日志丢失", true), LexicalSparse("排查日志丢失", true)) {
		t.Fatal("question stripping")
	}
	if len(LexicalSparse("怎么", true).Indices) == 0 {
		t.Fatal("question-only fallback")
	}
	if len(LexicalSparse(" !? ", true).Indices) != 0 {
		t.Fatal("empty terms")
	}
	terms := []string{}
	for i := 0; i < 55; i++ {
		terms = append(terms, fmt.Sprintf("term%02d", i))
	}
	q := LexicalSparse(strings.Join(terms, " "), true)
	if !reflect.DeepEqual(q, LexicalSparse(strings.Join(terms[:48], " "), true)) || len(q.Indices) != 48 {
		t.Fatal("query cap", q)
	}
	for i, id := range q.Indices {
		if q.Values[i] != 1 || (i > 0 && q.Indices[i-1] >= id) {
			t.Fatal("sorted indices and unit query weights", q)
		}
	}
}
