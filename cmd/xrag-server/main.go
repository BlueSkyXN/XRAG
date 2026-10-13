package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/BlueSkyXN/XRAG/core/types"
	"github.com/BlueSkyXN/XRAG/server/engine"
	"github.com/BlueSkyXN/XRAG/server/httpapi"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2:]); err != nil {
		if f := types.As(err); f != nil {
			fmt.Fprintf(os.Stderr, "%s\n", f.Error())
		} else {
			fmt.Fprintf(os.Stderr, "%s\n", err.Error())
		}
		os.Exit(1)
	}
}

func run(cmd string, args []string) error {
	switch cmd {
	case "init":
		fs := flag.NewFlagSet(cmd, flag.ExitOnError)
		dir := fs.String("data", "data", "data directory")
		_ = fs.Parse(args)
		eng, err := engine.Open(*dir)
		if err != nil {
			return err
		}
		return eng.Close()
	case "kb":
		return kb(args)
	case "source":
		return source(args)
	case "build":
		return build(args)
	case "keyword", "tree", "grep", "read", "page":
		return query(cmd, args)
	case "http":
		return serve(args)
	default:
		usage()
		return types.Fail("INVALID_ARGUMENT", "unknown command", 400)
	}
}

func kb(args []string) error {
	if len(args) == 0 || args[0] != "add" {
		return types.Fail("INVALID_ARGUMENT", "use: kb add", 400)
	}
	fs := flag.NewFlagSet("kb", flag.ExitOnError)
	dir := fs.String("data", "data", "data directory")
	id := fs.String("id", "", "knowledge base id")
	name := fs.String("name", "", "display name")
	_ = fs.Parse(args[1:])
	eng, err := engine.Open(*dir)
	if err != nil {
		return err
	}
	defer eng.Close()
	if *name == "" {
		*name = *id
	}
	return eng.CreateKB(context.Background(), *id, *name)
}

func source(args []string) error {
	if len(args) == 0 || args[0] != "add" {
		return types.Fail("INVALID_ARGUMENT", "use: source add", 400)
	}
	fs := flag.NewFlagSet("source", flag.ExitOnError)
	dir := fs.String("data", "data", "data directory")
	kb := fs.String("kb", "", "knowledge base id")
	id := fs.String("id", "", "source id")
	mount := fs.String("mount", "", "path mount")
	root := fs.String("path", "", "directory to scan")
	_ = fs.Parse(args[1:])
	eng, err := engine.Open(*dir)
	if err != nil {
		return err
	}
	defer eng.Close()
	return eng.AddPathSource(context.Background(), *kb, *id, *mount, *root)
}

func build(args []string) error {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	dir := fs.String("data", "data", "data directory")
	kb := fs.String("kb", "", "knowledge base id")
	_ = fs.Parse(args)
	eng, err := engine.Open(*dir)
	if err != nil {
		return err
	}
	defer eng.Close()
	summary, err := eng.Build(context.Background(), *kb, engine.Options{})
	if err != nil {
		return err
	}
	return printJSON(summary)
}

func query(cmd string, args []string) error {
	fs := flag.NewFlagSet(cmd, flag.ExitOnError)
	dir := fs.String("data", "data", "data directory")
	kb := fs.String("kb", "", "knowledge base id")
	q := fs.String("q", "", "query or pattern")
	ref := fs.String("ref", "", "xr1 ref")
	result := fs.String("result", "", "frozen result id")
	limit := fs.Int("limit", 10, "page size")
	offset := fs.Int("offset", 0, "page offset")
	_ = fs.Parse(args)
	eng, err := engine.Open(*dir)
	if err != nil {
		return err
	}
	defer eng.Close()
	ctx := context.Background()
	page := engine.Page{Limit: *limit, Offset: *offset}
	var resp types.Response
	switch cmd {
	case "keyword":
		resp, err = eng.Keyword(ctx, *kb, *q, page)
	case "tree":
		resp, err = eng.Tree(ctx, *kb)
	case "grep":
		resp, err = eng.Grep(ctx, *kb, *q)
	case "read":
		resp, err = eng.Read(ctx, *ref)
	case "page":
		resp, err = eng.Page(ctx, *result, page)
	}
	if err != nil {
		return err
	}
	return printJSON(resp)
}

func serve(args []string) error {
	fs := flag.NewFlagSet("http", flag.ExitOnError)
	dir := fs.String("data", "data", "data directory")
	addr := fs.String("addr", "127.0.0.1:8080", "listen address")
	_ = fs.Parse(args)
	eng, err := engine.Open(*dir)
	if err != nil {
		return err
	}
	defer eng.Close()
	server := &http.Server{Addr: *addr, Handler: (&httpapi.Server{Engine: eng}).Handler()}
	return server.ListenAndServe()
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func usage() {
	fmt.Fprintf(os.Stderr, `xrag-server is the M1 server: directory build, published lexical search, and edition-pinned read.

  xrag-server init -data data
  xrag-server kb add -data data -id demo -name Demo
  xrag-server source add -data data -kb demo -id docs -path ./corpus
  xrag-server build -data data -kb demo
  xrag-server keyword -data data -kb demo -q E203
  xrag-server read -data data -ref xr1:...
  xrag-server grep -data data -kb demo -q E203
  xrag-server tree -data data -kb demo
  xrag-server page -data data -result rs_... -offset 10

Graph, Wiki, Client modes, and Recipe are not part of this build.
`)
}
