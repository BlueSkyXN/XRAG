package core

import (
	"hash/fnv"
	"sort"
)

const LexicalSchema = "xrag-terms-fnv1a32-bm25-v1"
const LexicalVectorName = "xrag_terms"

// LexicalAverageLength 固定为 v1 demo 的 3231 个词元 / 51 个块。
// 修改分词、哈希或归一化常数时必须更换 LexicalSchema。
const LexicalAverageLength = 3231.0 / 51.0

type SparseVector struct {
	Indices []uint32  `json:"indices"`
	Values  []float64 `json:"values"`
}

// LexicalSparse 在文档侧计算 BM25 TF，在查询侧对不同词元赋值 1。
// IDF 由 Qdrant 的 sparse_vectors.modifier=idf 提供。
func LexicalSparse(text string, query bool) SparseVector {
	terms := Terms(text, false)
	if query {
		terms = Unique(Terms(StripQuestion(text), true))
		if len(terms) == 0 {
			terms = Unique(Terms(text, true))
		}
		if len(terms) > 48 {
			terms = terms[:48]
		}
	}
	frequencies := map[uint32]int{}
	for _, term := range terms {
		h := fnv.New32a()
		_, _ = h.Write([]byte(term))
		frequencies[h.Sum32()]++
	}
	v := SparseVector{Indices: []uint32{}, Values: []float64{}}
	for id := range frequencies {
		v.Indices = append(v.Indices, id)
	}
	sort.Slice(v.Indices, func(i, j int) bool { return v.Indices[i] < v.Indices[j] })
	const k1, b = 1.2, 0.75
	for _, id := range v.Indices {
		tf := float64(frequencies[id])
		value := tf
		if !query {
			value = tf * (k1 + 1) / (tf + k1*(1-b+b*float64(len(terms))/LexicalAverageLength))
		}
		v.Values = append(v.Values, value)
	}
	return v
}
