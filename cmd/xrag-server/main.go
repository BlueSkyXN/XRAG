package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/automation"
	"github.com/BlueSkyXN/XRAG/internal/build"
	"github.com/BlueSkyXN/XRAG/internal/housekeeping"
	"github.com/BlueSkyXN/XRAG/internal/maintenance"
	"github.com/BlueSkyXN/XRAG/internal/mcpauth"
	"github.com/BlueSkyXN/XRAG/internal/server"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) < 2 {
		fmt.Println("xrag-server init|serve|worker|backup|restore|prune|vector-gc|recover-projections  --data .xrag\ninit --examples ./examples/docs; serve --config config.json --workers off")
		return nil
	}
	cmd := os.Args[1]
	if cmd == "version" {
		fmt.Println(core.Version)
		return nil
	}
	f := flag.NewFlagSet(cmd, flag.ContinueOnError)
	data := f.String("data", ".xrag", "data directory")
	listen := f.String("listen", "127.0.0.1:8765", "listen address")
	config := f.String("config", "", "provider configuration JSON")
	mcpAuthFile := f.String("mcp-auth", "", "optional OAuth protected-resource configuration JSON")
	examples := f.String("examples", "", "copy initial MD/TXT examples")
	webEnabled := f.Bool("web", true, "serve the bundled workbench")
	mcpEnabled := f.Bool("mcp", true, "serve MCP endpoint")
	workers := f.String("workers", "embedded", "embedded or off")
	buildKB := f.String("build", "", "explicitly enqueue a build when starting")
	targetsFile := f.String("targets", "", "explicit recovery backend bindings JSON")
	dest := f.String("out", "", "new backup SQLite path")
	from := f.String("from", "", "SQLite snapshot to restore into a NEW data directory")
	keepDays := f.Int("keep-days", 30, "offline prune: minimum days since retirement")
	cleanupExternal := f.Bool("external", false, "offline prune: explicitly delete recorded external projections before local history")
	dropModel := f.Bool("drop-model-cache", false, "offline prune: also drop rebuildable model cache (may cause future model calls)")
	confirm := f.Bool("confirm", false, "explicitly confirm restore or offline pruning")
	scan := f.Duration("scan-every", 0, "explicit periodic incremental scan interval; no filesystem watcher implied")
	if e := f.Parse(os.Args[2:]); e != nil {
		return e
	}
	if *workers != "embedded" && *workers != "off" {
		return fmt.Errorf("--workers must be embedded or off")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if cmd == "restore" {
		if !*confirm || *from == "" {
			return fmt.Errorf("restore requires --from SNAPSHOT --data NEW_DIRECTORY --confirm")
		}
		var explicitData bool
		f.Visit(func(v *flag.Flag) {
			if v.Name == "data" {
				explicitData = true
			}
		})
		if !explicitData {
			return fmt.Errorf("restore requires an explicit NEW --data directory")
		}
		v, e := maintenance.Restore(ctx, *from, *data)
		if e != nil {
			return e
		}
		fmt.Println(core.JSON(v))
		return nil
	}
	if cmd == "prune" || cmd == "vector-gc" || cmd == "recover-projections" {
		if st, e := os.Stat(filepath.Join(*data, "xrag.sqlite")); e != nil || !st.Mode().IsRegular() {
			return fmt.Errorf("prune requires an existing data directory")
		}
	}
	unlock, e := maintenance.RuntimeLock(*data, cmd == "prune" || cmd == "vector-gc" || cmd == "recover-projections")
	if e != nil {
		return e
	}
	defer unlock()
	s, e := store.Open(*data)
	if e != nil {
		return e
	}
	defer s.Close()
	logger := log.New(os.Stderr, "xrag ", log.LstdFlags)
	cfg, e := adapters.ReadConfig(*config)
	if e == nil && *config == "" {
		setting, err := s.ProviderSetting(ctx)
		if err != nil {
			return err
		}
		cfg, e = adapters.ParseConfig(setting.Data)
	}
	if e != nil {
		return e
	}
	if cmd == "recover-projections" {
		if *targetsFile == "" {
			return fmt.Errorf("recover-projections --data RESTORED_DIRECTORY --targets FILE [--confirm]")
		}
		raw, er := os.ReadFile(*targetsFile)
		if er != nil {
			return er
		}
		if len(raw) > 65536 {
			return fmt.Errorf("recovery target file exceeds 64 KiB")
		}
		targets, er := build.DecodeRecoveryTargets(raw)
		if er != nil {
			return er
		}
		result, er := (&build.Builder{Store: s}).RecoverProjections(ctx, targets, *confirm)
		fmt.Println(core.JSON(result))
		return er
	}
	if cmd == "serve" || cmd == "worker" {
		if er := s.CheckRecoveryComplete(ctx); er != nil {
			return er
		}
	}
	if cmd == "vector-gc" {
		result, err := (&build.Builder{Store: s, Config: cfg}).CollectVectorGenerations(ctx, *keepDays, *confirm)
		fmt.Println(core.JSON(result))
		return err
	}
	if cmd == "prune" {
		if *confirm && !*cleanupExternal && (cfg.Qdrant != nil || cfg.Neo4j != nil) {
			return fmt.Errorf("offline prune cannot clean configured external projections; preview only")
		}
		// Also reject a legacy API instance which does not yet hold runtime.lock.
		l, err := os.OpenFile(filepath.Join(s.Dir, "server.lock"), os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			return err
		}
		defer l.Close()
		if err = syscall.Flock(int(l.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
			return fmt.Errorf("stop the API before pruning")
		}
		defer syscall.Flock(int(l.Fd()), syscall.LOCK_UN)
		var result store.PruneReport
		if *cleanupExternal {
			result, err = (&build.Builder{Store: s, Config: cfg}).PruneExternal(ctx, *keepDays, *confirm, *dropModel)
		} else {
			result, err = s.PruneHistory(ctx, *keepDays, *confirm, *dropModel)
		}
		if err != nil {
			return err
		}
		fmt.Println(core.JSON(result))
		return nil
	}
	builder := &build.Builder{Store: s, Config: cfg, Logger: logger}
	switch cmd {
	case "init":
		n, e := s.CountKeys(ctx)
		if e != nil {
			return e
		}
		if n == 0 {
			token, e := s.CreateKey(ctx, "initial-admin", "admin", nil)
			if e != nil {
				return e
			}
			if e = os.WriteFile(filepath.Join(s.Dir, "admin.key"), []byte(token+"\n"), 0600); e != nil {
				return e
			}
			if e = s.CreateKB(ctx, "demo", "演示知识库"); e != nil {
				return e
			}
			if *examples != "" {
				dst := filepath.Join(s.Dir, "sources", "demo")
				if e = copyExamples(*examples, dst); e != nil {
					return e
				}
				if e = s.AddSource(ctx, store.Source{KB: "demo", ID: "manual", Root: dst, Mount: "manual", Kind: "path"}); e != nil {
					return e
				}
			}
			fmt.Println("Initialized " + s.Dir + "\nKey file: " + filepath.Join(s.Dir, "admin.key"))
		} else {
			fmt.Println("Already initialized; no data or keys were replaced.")
		}
		return nil
	case "backup":
		if *dest == "" {
			return fmt.Errorf("--out required")
		}
		if e = s.Backup(ctx, *dest); e != nil {
			return e
		}
		fmt.Println("Database backup written. Source files/config need a separate backup: " + *dest)
		return nil
	case "serve", "worker":
	default:
		return fmt.Errorf("unknown server command %s", cmd)
	}
	// One API process per data directory; workers coordinate using SQLite jobs and per-KB running uniqueness.
	var lock *os.File
	if cmd == "serve" {
		lock, e = os.OpenFile(filepath.Join(s.Dir, "server.lock"), os.O_CREATE|os.O_RDWR, 0600)
		if e != nil {
			return e
		}
		defer lock.Close()
		if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
			return fmt.Errorf("another API server is using this data directory")
		}
		defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	}
	wg := &sync.WaitGroup{}
	if cmd == "worker" || *workers == "embedded" {
		wg.Add(1)
		go builder.Loop(ctx, wg)
		wg.Add(1)
		go (&housekeeping.Controller{Store: s, Logger: logger}).Loop(ctx, wg)
	}
	defer func() { stop(); wg.Wait() }()
	if cmd == "serve" {
		wg.Add(1)
		go (&automation.Controller{Builder: builder}).Loop(ctx, wg)
	}
	if *buildKB != "" {
		if _, e = s.Enqueue(ctx, *buildKB, "build", "server-start", "", map[string]any{"mode": "incremental"}); e != nil {
			return e
		}
	}
	if *scan > 0 && cmd == "serve" {
		if *scan < 10*time.Second {
			return fmt.Errorf("--scan-every minimum is 10s")
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			t := time.NewTicker(*scan)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					rs, e := s.KBs(ctx)
					if e != nil {
						continue
					}
					for _, r := range rs {
						_, _ = s.Enqueue(ctx, r["id"].(string), "build", "scheduled", "", map[string]any{"mode": "incremental"})
					}
				}
			}
		}()
	}
	if cmd == "worker" {
		logger.Print("worker started; no HTTP listener")
		<-ctx.Done()
		return nil
	}
	app := server.New(s, builder, logger)
	app.MCPAuth, e = mcpauth.Read(*mcpAuthFile)
	if e != nil {
		return e
	}
	app.MCPTransport.Lifetime = ctx
	app.Workers = *workers == "embedded"
	app.DisableWeb = !*webEnabled
	app.DisableMCP = !*mcpEnabled
	app.ConfigSource = "managed"
	if *config != "" {
		app.ConfigSource = "external_file"
	}
	if os.Getenv("XRAG_EDGE_LOGIN") == "1" {
		app.EdgeUser = os.Getenv("ADMIN_USERNAME")
		app.EdgePass = os.Getenv("ADMIN_PASSWORD")
		if app.EdgeUser == "" || app.EdgePass == "" {
			return fmt.Errorf("XRAG_EDGE_LOGIN requires ADMIN_USERNAME and ADMIN_PASSWORD")
		}
	}
	srv := &http.Server{Addr: *listen, Handler: app, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 120 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	errc := make(chan error, 1)
	go func() {
		logger.Printf("API + Studio http://%s | workers=%s | models only when configured", *listen, *workers)
		errc <- srv.ListenAndServe()
	}()
	select {
	case e := <-errc:
		if e != http.ErrServerClosed {
			return e
		}
	case <-ctx.Done():
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(c)
	}
	return nil
}
func copyExamples(src, dst string) error {
	src, e := filepath.Abs(src)
	if e != nil {
		return e
	}
	return filepath.Walk(src, func(p string, st os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(src, p)
		if e != nil {
			return e
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		out := filepath.Join(dst, rel)
		if st.IsDir() {
			return os.MkdirAll(out, 0700)
		}
		ext := strings.ToLower(filepath.Ext(p))
		if ext != ".md" && ext != ".txt" {
			return nil
		}
		in, e := os.Open(p)
		if e != nil {
			return e
		}
		defer in.Close()
		f, e := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return e
		}
		defer f.Close()
		_, e = io.Copy(f, in)
		return e
	})
}
