package core

import (
	"fmt"
	"strings"
	"testing"
)

func TestRelaxedDictionaryQuery(t *testing.T) {
	cases := []struct {
		name, query, want string
		words             []string
	}{
		{"question", "日志丢失怎么排查", `"日志" OR "志丢" OR "丢失" OR "排查"`, nil},
		{"chinese stop words", "怎么样 怎么 怎样 如何 什么 哪些 哪个 哪里 为什么 为何 是否 能否 可以 请问 有没有 是不是 的 了 吗 呢 吧 是 E203", `"e203"`, nil},
		{"english stop words", "WHAT how Why which where who is are does do the a an of to in for E203", `"e203"`, nil},
		{"whole words", "WHAT theory format within E203 设备A", `"theory" OR "format" OR "within" OR "e203" OR "设备" OR "a"`, nil},
		{"deduplicate", "E203 E203 怎么 E203", `"e203"`, nil},
		{"chinese fallback", "怎么", `"怎么"`, nil},
		{"english fallback", "what is", `"what" OR "is"`, nil},
		{"punctuation fallback", "怎么？", `"怎么"`, nil},
		{"dictionary", "如何排查日志丢失", `"domain` + Hash("日志丢失")[:24] + `" OR "排查"`, []string{"日志丢失"}},
		{"unknown identifier question", "ZXQ884422VOID 是什么", `"zxq884422void"`, nil},
		{"single Han question", "锁是什么", `"锁"`, nil},
		{"only single Han terms", "锁 门是什么", `"锁" OR "门"`, nil},
		{"Han bigram removes single Han", "锁 日志", `"日志"`, nil},
		{"identifier removes single Han", "锁 E203", `"e203"`, nil},
		{"number removes single Han", "锁 203", `"203"`, nil},
		{"keep single Latin and digit", "锁 X 7 日志", `"x" OR "7" OR "日志"`, nil},
		{"dictionary removes single Han", "锁 泵", `"domain` + Hash("锁")[:24] + `"`, []string{"锁"}},
		{"copula fallback", "是", `"是"`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := RelaxedDictionaryQuery(tc.query, tc.words)
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestRelaxedDictionaryQueryTermLimit(t *testing.T) {
	terms := make([]string, 60)
	for i := range terms {
		terms[i] = fmt.Sprintf("token%02d", i)
	}
	got, err := RelaxedDictionaryQuery(strings.Join(terms, " "), nil)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(got, " OR ")
	if len(parts) != 48 || parts[0] != `"token00"` || parts[47] != `"token47"` {
		t.Fatal(got)
	}
	if _, err := DictionaryQuery(strings.Join(terms, " "), nil); err == nil {
		t.Fatal("strict query must retain its term limit")
	}
	if _, err := RelaxedDictionaryQuery(" ?! ", nil); err == nil {
		t.Fatal("empty lexical query must still fail")
	}
}
