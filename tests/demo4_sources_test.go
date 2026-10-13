package tests

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/BlueSkyXN/XRAG/internal/source"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

func TestS3SignatureMatchesIndependentBotocoreVector(t *testing.T) {
	r, _ := http.NewRequest("GET", "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	r.Header.Set("Range", "bytes=0-9")
	source.SignS3(r, "AKIAIOSFODNN7EXAMPLE", "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY", "", "us-east-1", time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC))
	if !strings.HasSuffix(r.Header.Get("Authorization"), "Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41") {
		t.Fatal(r.Header.Get("Authorization"))
	}
}
func s3src(url string) store.Source {
	return store.Source{KB: "demo", ID: "s3docs", Kind: "s3", Root: url, Mount: "remote", Remote: &store.RemoteSource{Bucket: "docs-bucket", Prefix: "notes/", Region: "us-east-1", Anonymous: true}}
}
func TestS3CompletePaginationAndConditionalRead(t *testing.T) {
	var gets, lists atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("list-type") == "2" {
			lists.Add(1)
			w.Header().Set("Content-Type", "application/xml")
			if r.URL.Query().Get("continuation-token") == "" {
				io.WriteString(w, `<ListBucketResult><EncodingType>url</EncodingType><IsTruncated>true</IsTruncated><NextContinuationToken>second+/=</NextContinuationToken><Contents><Key>notes%2F%E4%B8%AD%E6%96%87.md</Key><ETag>"one"</ETag><Size>8</Size></Contents></ListBucketResult>`)
			} else {
				if r.URL.Query().Get("continuation-token") != "second+/=" {
					t.Error("token changed")
				}
				io.WriteString(w, `<ListBucketResult><EncodingType>url</EncodingType><IsTruncated>false</IsTruncated><Contents><Key>notes%2Ftwo.txt</Key><ETag>"two"</ETag><Size>3</Size></Contents></ListBucketResult>`)
			}
			return
		}
		gets.Add(1)
		if strings.HasSuffix(r.URL.Path, "中文.md") {
			if r.Header.Get("If-Match") != `"one"` {
				t.Error("missing conditional read")
			}
			w.Header().Set("ETag", `"one"`)
			fmt.Fprint(w, "# 中文")
		} else {
			w.Header().Set("ETag", `"two"`)
			fmt.Fprint(w, "two")
		}
	}))
	defer srv.Close()
	scan, e := source.Open(ctx, t.TempDir(), s3src(srv.URL))
	must(t, e)
	if len(scan.Entries) != 2 || gets.Load() != 2 || lists.Load() != 4 || scan.Consistency != "object_conditional_live" {
		t.Fatalf("%+v get=%d list=%d", scan, gets.Load(), lists.Load())
	}
}
func TestS3FailureDoesNotRemovePublishedFiles(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("list-type") == "2" {
			fmt.Fprint(w, `<ListBucketResult><IsTruncated>true</IsTruncated><Contents><Key>notes/a.md</Key><ETag>"a"</ETag><Size>1</Size></Contents></ListBucketResult>`)
			return
		}
		w.WriteHeader(412)
	}))
	defer srv.Close()
	s, _ := setup(t)
	src := s3src(srv.URL)
	must(t, s.AddSource(ctx, src))
	pub(t, s, "remote/old.md", "# Old\nKeep this original.")
	_, e := source.Open(ctx, s.Dir, src)
	code(t, e, "SOURCE_INVALID")
	v, e := s.Snapshot(ctx, []string{"demo"})
	must(t, e)
	if len(v.Documents) != 1 {
		t.Fatal("lost previous source")
	}
}
func TestS3ChangedObjectAndRedirectRejected(t *testing.T) {
	redirect := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if redirect {
			http.Redirect(w, r, "https://example.invalid/steal", 302)
			return
		}
		if r.URL.Query().Get("list-type") == "2" {
			fmt.Fprint(w, `<ListBucketResult><IsTruncated>false</IsTruncated><Contents><Key>notes/a.md</Key><ETag>"a"</ETag><Size>1</Size></Contents></ListBucketResult>`)
		} else {
			w.WriteHeader(412)
		}
	}))
	defer srv.Close()
	_, e := source.Open(ctx, t.TempDir(), s3src(srv.URL))
	code(t, e, "SOURCE_CHANGED")
	redirect = true
	_, e = source.Open(ctx, t.TempDir(), s3src(srv.URL))
	code(t, e, "SOURCE_UNAVAILABLE")
}
func TestRemoteSourceConfigReferencesAndRoundtrip(t *testing.T) {
	s, _ := setup(t)
	src := s3src("https://s3.example.test")
	src.Remote.Anonymous = false
	src.Remote.AccessKey = "env:S3_ACCESS"
	src.Remote.SecretKey = "file:/tmp/secret"
	must(t, s.AddSource(ctx, src))
	all, e := s.Sources(ctx, "demo")
	must(t, e)
	if len(all) != 1 || all[0].Remote.SecretKey != "file:/tmp/secret" {
		t.Fatal(all)
	}
	src.ID = "bad"
	src.Mount = "bad"
	src.Remote.SecretKey = "plaintext"
	code(t, s.AddSource(ctx, src), "INVALID_ARGUMENT")
	src.Kind = "path"
	src.Root = t.TempDir()
	code(t, s.AddSource(ctx, src), "INVALID_ARGUMENT")
}
func wkSource(url string) store.Source {
	return store.Source{KB: "demo", ID: "wk", Mount: "wiki-source", Kind: "weknora", Root: url, Remote: &store.RemoteSource{KnowledgeBase: "upstream", APIKey: "env:WK_TEST_KEY", AuthHeader: "X-API-Key"}}
}
func TestWeKnoraParsedTextExportKeepsStableDocumentIdentity(t *testing.T) {
	t.Setenv("WK_TEST_KEY", "fixture-key")
	var lists atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "fixture-key" {
			t.Error("key missing")
		}
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "knowledge-bases") {
			lists.Add(1)
			json.NewEncoder(w).Encode(map[string]any{"success": true, "page": 1, "page_size": 100, "total": 1, "data": []any{map[string]any{"id": "doc-01", "knowledge_base_id": "upstream", "title": "日志资料", "parse_status": "completed", "enable_status": "enabled", "updated_at": "2026-10-12T00:00:00Z"}}})
		} else {
			if r.URL.Query().Get("chunk_type") != "text" {
				t.Error("wrong chunk kind")
			}
			json.NewEncoder(w).Encode(map[string]any{"success": true, "page": 1, "page_size": 100, "total": 2, "data": []any{map[string]any{"id": "c2", "knowledge_id": "doc-01", "content": "第二段", "chunk_index": 1, "chunk_type": "text", "is_enabled": true}, map[string]any{"id": "c1", "knowledge_id": "doc-01", "content": "第一段", "chunk_index": 0, "chunk_type": "text", "is_enabled": true}}})
		}
	}))
	defer srv.Close()
	scan, e := source.Open(ctx, t.TempDir(), wkSource(srv.URL))
	must(t, e)
	if len(scan.Entries) != 1 || scan.Entries[0].Path != "doc-01.md" || lists.Load() != 2 {
		t.Fatal(scan)
	}
	body := scan.Entries[0].Body
	if strings.Index(body, "第一段") > strings.Index(body, "第二段") || !strings.Contains(body, "not the original file") {
		t.Fatal(body)
	}
}
func TestWeKnoraPendingOrUnknownProtocolKeepsPreviousPublication(t *testing.T) {
	t.Setenv("WK_TEST_KEY", "fixture-key")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"success": true, "page": 1, "page_size": 100, "total": 1, "data": []any{map[string]any{"id": "doc-01", "knowledge_base_id": "upstream", "parse_status": "processing"}}})
	}))
	defer srv.Close()
	_, e := source.Open(ctx, t.TempDir(), wkSource(srv.URL))
	code(t, e, "SOURCE_NOT_READY")
}
func TestS3SignedRequestsDoNotLeakSecrets(t *testing.T) {
	t.Setenv("S3A", "test-access")
	t.Setenv("S3S", "never-send-secret")
	t.Setenv("S3T", "temp-session")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if !strings.Contains(h, "Credential=test-access/") || strings.Contains(h, "never-send-secret") || r.Header.Get("X-Amz-Security-Token") != "temp-session" {
			t.Error(h)
		}
		fmt.Fprint(w, `<ListBucketResult><IsTruncated>false</IsTruncated></ListBucketResult>`)
	}))
	defer srv.Close()
	s := s3src(srv.URL)
	s.Remote.Anonymous = false
	s.Remote.AccessKey = "env:S3A"
	s.Remote.SecretKey = "env:S3S"
	s.Remote.SessionToken = "env:S3T"
	_, e := source.Open(ctx, t.TempDir(), s)
	must(t, e)
}
func TestS3InvalidAndRepeatedPathsRefused(t *testing.T) {
	for _, key := range []string{"notes/../a.md", "outside/a.md"} {
		t.Run(url.PathEscape(key), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `<ListBucketResult><IsTruncated>false</IsTruncated><Contents><Key>%s</Key><ETag>"a"</ETag><Size>1</Size></Contents></ListBucketResult>`, key)
			}))
			defer srv.Close()
			_, e := source.Open(ctx, t.TempDir(), s3src(srv.URL))
			if e == nil {
				t.Fatal("accepted invalid key")
			}
		})
	}
}
func TestWeKnoraSourceConfigRoundtrip(t *testing.T) {
	s, _ := setup(t)
	src := wkSource("https://wiki.example.test")
	must(t, s.AddSource(ctx, src))
	items, e := s.Sources(ctx, "demo")
	must(t, e)
	if items[0].Remote.KnowledgeBase != "upstream" {
		t.Fatal(items)
	}
}
