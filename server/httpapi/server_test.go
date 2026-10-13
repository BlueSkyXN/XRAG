package httpapi

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BlueSkyXN/XRAG/server/engine"
)

func TestHealthSetupUploadAndSearch(t *testing.T) {
	eng, err := engine.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { eng.Close() })
	srv := httptest.NewServer((&Server{Engine: eng}).Handler())
	t.Cleanup(srv.Close)
	health, err := http.Get(srv.URL + "/health")
	if err != nil || health.StatusCode != 200 {
		t.Fatal(err, health)
	}
	postJSON(t, srv.URL+"/api/setup", `{"kb":"demo","name":"Demo"}`)
	body := &bytes.Buffer{}
	form := multipart.NewWriter(body)
	_ = form.WriteField("kb", "demo")
	part, err := form.CreateFormFile("file", "note.md")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = part.Write([]byte("这里有故障编号 E203\n"))
	_ = form.Close()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/files", body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", form.FormDataContentType())
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != 200 {
		t.Fatal(err, res)
	}
	postJSON(t, srv.URL+"/api/build", `{"kb":"demo"}`)
	got, err := http.Get(srv.URL + "/api/keyword?kb=demo&q=E203")
	if err != nil {
		t.Fatal(err)
	}
	defer got.Body.Close()
	raw := new(bytes.Buffer)
	_, _ = raw.ReadFrom(got.Body)
	if got.StatusCode != 200 || !strings.Contains(raw.String(), "E203") {
		t.Fatalf("search: %s", raw.String())
	}
	bad := &bytes.Buffer{}
	badForm := multipart.NewWriter(bad)
	_ = badForm.WriteField("kb", "demo")
	part, _ = badForm.CreateFormFile("file", "secret.exe")
	_, _ = part.Write([]byte("nope"))
	_ = badForm.Close()
	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/api/files", bad)
	req.Header.Set("Content-Type", badForm.FormDataContentType())
	res, err = http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != 400 {
		t.Fatalf("traversal status %v %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(eng.Root(), "corpus", "demo", "secret.exe")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func postJSON(t *testing.T, url, payload string) {
	t.Helper()
	res, err := http.Post(url, "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		raw := new(bytes.Buffer)
		_, _ = raw.ReadFrom(res.Body)
		t.Fatalf("%s: %s", url, raw.String())
	}
}
