package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
)

func TestSyntheticRetrievalBoundaries(t *testing.T) {
	var fixture struct {
		Documents []struct{ Path, Body string }
		Cases     []struct {
			ID, Q, Action, Expected, Excluded, Release string
			Empty                                      bool
			ReadContains                               string `json:"read_contains"`
		}
	}
	raw, err := os.ReadFile("testdata/retrieval-boundaries.json")
	must(t, err)
	must(t, json.Unmarshal(raw, &fixture))
	s, server := setup(t)
	root := t.TempDir()
	for _, doc := range fixture.Documents {
		pub(t, s, doc.Path, doc.Body)
		must(t, os.WriteFile(filepath.Join(root, doc.Path), []byte(doc.Body), 0600))
	}
	local := &core.Engine{Repo: &adapters.Local{Root: root, Origin: "synthetic-boundaries", KB: "demo", CacheDir: t.TempDir()}, Mode: "local"}
	for _, engine := range []*core.Engine{server, local} {
		t.Run(engine.Mode, func(t *testing.T) {
			for _, tc := range fixture.Cases {
				t.Run(tc.ID, func(t *testing.T) {
					request := core.Request{Q: tc.Q, Limit: 5}
					if tc.Release != "" {
						request.Filters = []core.Filter{{Field: "fields.release", Op: "eq", Value: tc.Release}}
					}
					got := run(t, engine, tc.Action, request)
					paths := []string{}
					found := false
					for _, item := range got.Items {
						paths = append(paths, item.Path)
						if tc.Excluded != "" && item.Path == tc.Excluded {
							t.Errorf("unexpected path %s for %q", item.Path, tc.Q)
						}
						if item.Path == tc.Expected {
							found = true
							if tc.ReadContains != "" {
								read := run(t, engine, "files.read", core.Request{Ref: item.Ref})
								if !strings.Contains(core.JSON(read.Data), tc.ReadContains) {
									t.Fatalf("reference lost original condition: %s", core.JSON(read.Data))
								}
							}
						}
					}
					if tc.Empty && got.Returned != 0 || !tc.Empty && !found {
						t.Fatalf("query %q returned %v; expected %q, empty=%v", tc.Q, paths, tc.Expected, tc.Empty)
					}
				})
			}
		})
	}
}
