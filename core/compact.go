package core

import (
	"container/heap"
	"context"
	"sort"
)

// CompactReader loads immutable query metadata, not corpus bodies or vectors.
// refs=nil selects the current published files in one read transaction. Non-nil
// refs select those exact editions, never the current edition at the same path.
// Wiki bodies are requested only for the current context substring semantics.
// Implementations also implement IndexedReader for bounded page hydration.
// CompactSemantic explicitly opts in to body/vector-free Views. Existing
// custom Semantic implementations continue receiving full Views.
type CompactSemantic interface{ CompactSemantic() }

type CompactReader interface {
	QueryView(context.Context, []string, []Ref, bool) (View, error)
}

// RankWindow retains exact top-k with deterministic ID tie-breaking. Input is
// streamed; memory does not grow with the number of candidate vectors.
type RankWindow struct {
	Limit  int
	values rankHeap
}
type rankHeap []Ranked

func (h rankHeap) Len() int { return len(h) }
func (h rankHeap) Less(i, j int) bool {
	if h[i].Score == h[j].Score {
		return h[i].ID > h[j].ID
	}
	return h[i].Score < h[j].Score
}
func (h rankHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *rankHeap) Push(x any)   { *h = append(*h, x.(Ranked)) }
func (h *rankHeap) Pop() any     { a := *h; n := len(a); x := a[n-1]; *h = a[:n-1]; return x }
func (w *RankWindow) Add(x Ranked) {
	if w.Limit <= 0 {
		return
	}
	if len(w.values) < w.Limit {
		heap.Push(&w.values, x)
		return
	}
	worst := w.values[0]
	if x.Score > worst.Score || x.Score == worst.Score && x.ID < worst.ID {
		w.values[0] = x
		heap.Fix(&w.values, 0)
	}
}
func (w *RankWindow) Sorted() []Ranked {
	out := append([]Ranked{}, w.values...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].ID < out[j].ID
		}
		return out[i].Score > out[j].Score
	})
	return out
}
