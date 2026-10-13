package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	_ "embed"
	"github.com/BlueSkyXN/XRAG/core/types"
	"github.com/BlueSkyXN/XRAG/server/engine"
)

//go:embed web.html
var page []byte

type Server struct {
	Engine *engine.Engine
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(page)
	})
	mux.HandleFunc("POST /api/setup", s.setup)
	mux.HandleFunc("POST /api/files", s.upload)
	mux.HandleFunc("POST /api/build", s.build)
	mux.HandleFunc("GET /api/keyword", s.keyword)
	mux.HandleFunc("GET /api/tree", s.tree)
	mux.HandleFunc("GET /api/grep", s.grep)
	mux.HandleFunc("POST /api/read", s.read)
	mux.HandleFunc("GET /api/page", s.page)
	return mux
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	var body struct {
		KB   string `json:"kb"`
		Name string `json:"name"`
	}
	if !decode(w, r, &body) {
		return
	}
	if body.Name == "" {
		body.Name = body.KB
	}
	dir := filepath.Join(s.Engine.Root(), "corpus", body.KB)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fail(w, err)
		return
	}
	if err := s.Engine.CreateKB(r.Context(), body.KB, body.Name); err != nil && !exists(err) {
		fail(w, err)
		return
	}
	if err := s.Engine.AddPathSource(r.Context(), body.KB, "docs", "docs", dir); err != nil && !exists(err) {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"kb": body.KB, "corpus": dir})
}

func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(2 << 20); err != nil {
		fail(w, types.Fail("INVALID_ARGUMENT", "upload is too large or malformed", 400))
		return
	}
	kb := r.FormValue("kb")
	file, header, err := r.FormFile("file")
	if err != nil {
		fail(w, types.Fail("INVALID_ARGUMENT", "file is required", 400))
		return
	}
	defer file.Close()
	name := filepath.Base(header.Filename)
	ext := strings.ToLower(filepath.Ext(name))
	if header.Filename != name || strings.ContainsAny(header.Filename, `\/`) || name == "." || (ext != ".md" && ext != ".txt") {
		fail(w, types.Fail("INVALID_ARGUMENT", "only a markdown or text filename is accepted", 400))
		return
	}
	dir := filepath.Join(s.Engine.Root(), "corpus", kb)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fail(w, err)
		return
	}
	target := filepath.Join(dir, name)
	if !strings.HasPrefix(target, dir+string(os.PathSeparator)) {
		fail(w, types.Fail("INVALID_ARGUMENT", "filename escapes the corpus", 400))
		return
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		fail(w, err)
		return
	}
	defer out.Close()
	if _, err := io.Copy(out, io.LimitReader(file, 2<<20+1)); err != nil {
		fail(w, err)
		return
	}
	info, err := out.Stat()
	if err != nil {
		fail(w, err)
		return
	}
	if info.Size() > 2<<20 {
		_ = os.Remove(target)
		fail(w, types.Fail("INVALID_ARGUMENT", "file exceeds 2 MiB", 400))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"kb": kb, "file": name})
}

func (s *Server) build(w http.ResponseWriter, r *http.Request) {
	var body struct {
		KB string `json:"kb"`
	}
	if !decode(w, r, &body) {
		return
	}
	summary, err := s.Engine.Build(r.Context(), body.KB, engine.Options{})
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) keyword(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Engine.Keyword(r.Context(), r.URL.Query().Get("kb"), r.URL.Query().Get("q"), pageQuery(r))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) tree(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Engine.Tree(r.Context(), r.URL.Query().Get("kb"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) grep(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Engine.Grep(r.Context(), r.URL.Query().Get("kb"), r.URL.Query().Get("q"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) read(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Ref string `json:"ref"`
	}
	if !decode(w, r, &body) {
		return
	}
	resp, err := s.Engine.Read(r.Context(), body.Ref)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) page(w http.ResponseWriter, r *http.Request) {
	resp, err := s.Engine.Page(r.Context(), r.URL.Query().Get("result"), pageQuery(r))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func pageQuery(r *http.Request) engine.Page {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	return engine.Page{Limit: limit, Offset: offset}
}

func decode(w http.ResponseWriter, r *http.Request, dest any) bool {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		fail(w, types.Fail("INVALID_ARGUMENT", "request JSON is invalid", 400))
		return false
	}
	return true
}

func exists(err error) bool {
	text := err.Error()
	return strings.Contains(text, "UNIQUE") || strings.Contains(text, "constraint")
}

func fail(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	var f *types.Failure
	if errors.As(err, &f) {
		status = f.Status
		if status == 0 {
			status = http.StatusBadRequest
		}
		writeJSON(w, status, map[string]string{"error": f.Error()})
		return
	}
	writeJSON(w, status, map[string]string{"error": "internal error"})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
