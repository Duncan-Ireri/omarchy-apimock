// Package control implements the JSON-lines protocol the Omarchy plugin uses to
// drive a mock server: one JSON object per line on stdin, one per line on stdout.
package control

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/duncanireri/apimock/mock"
)

const protocolVersion = 1

// Runner owns a single embedded mock server across its lifetime.
type Runner struct {
	version string

	encMu sync.Mutex
	enc   *json.Encoder

	mu          sync.Mutex
	source      string
	host        string
	port        int
	journalSize int
	journal     *mock.Journal
	store       *mock.Store
	server      *mock.Server
}

// Run reads commands until stdin closes or a shutdown command arrives.
func Run(version string, in io.Reader, out io.Writer) error {
	r := &Runner{
		version:     version,
		enc:         json.NewEncoder(out),
		host:        "127.0.0.1",
		port:        8080,
		journalSize: 200,
	}
	r.journal = mock.NewJournal(r.journalSize)

	r.emit(map[string]any{"ev": "ready", "protocol": protocolVersion, "version": version})
	r.pushState()

	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var cmd map[string]any
		if err := json.Unmarshal([]byte(line), &cmd); err != nil {
			r.emitLog("warn", "ignored malformed command: "+err.Error())
			continue
		}
		if r.dispatch(cmd) {
			break
		}
	}
	r.stopServer()
	return sc.Err()
}

func (r *Runner) dispatch(cmd map[string]any) (shutdown bool) {
	id, _ := cmd["id"].(float64)
	name, _ := cmd["cmd"].(string)

	switch name {
	case "hello":
		r.reply(id, map[string]any{"ok": true, "version": r.version, "protocol": protocolVersion})

	case "configure":
		r.configure(cmd)
		r.reply(id, map[string]any{"ok": true})
		r.pushState()

	case "start":
		err := r.startServer()
		r.replyErr(id, err)
		r.pushState()

	case "stop":
		r.stopServer()
		r.reply(id, map[string]any{"ok": true})
		r.pushState()

	case "reload":
		err := r.reload()
		r.replyErr(id, err)
		r.pushState()

	case "status":
		st := r.stateMap()
		delete(st, "ev")
		st["ok"] = true
		r.reply(id, st)

	case "journal":
		limit := 0
		if v, ok := cmd["limit"].(float64); ok {
			limit = int(v)
		}
		r.mu.Lock()
		entries := r.journal.Recent(limit)
		r.mu.Unlock()
		r.reply(id, map[string]any{"ok": true, "entries": entries})

	case "clearJournal":
		r.mu.Lock()
		r.journal.Clear()
		r.mu.Unlock()
		r.reply(id, map[string]any{"ok": true})
		r.pushState()

	case "shutdown":
		r.reply(id, map[string]any{"ok": true})
		return true

	default:
		r.replyErr(id, fmt.Errorf("unknown command %q", name))
	}
	return false
}

func (r *Runner) configure(cmd map[string]any) {
	r.mu.Lock()
	changed := false
	autoStart := false
	if v, ok := cmd["source"].(string); ok && v != r.source {
		r.source = v
		changed = true
	}
	if v, ok := cmd["host"].(string); ok && v != "" && v != r.host {
		r.host = v
		changed = true
	}
	if v, ok := cmd["port"].(float64); ok && int(v) != r.port && int(v) > 0 {
		r.port = int(v)
		changed = true
	}
	if v, ok := cmd["journalSize"].(float64); ok && int(v) > 0 {
		r.journalSize = int(v)
	}
	if v, ok := cmd["autoStart"].(bool); ok {
		autoStart = v
	}
	running := r.server != nil && r.server.Running()
	r.mu.Unlock()

	switch {
	case changed && running:
		r.stopServer()
		if err := r.startServer(); err != nil {
			r.emitLog("warn", "restart after configure failed: "+err.Error())
		}
	case autoStart && !running:
		if err := r.startServer(); err != nil {
			r.emitLog("warn", "autostart failed: "+err.Error())
		}
	}
}

func (r *Runner) startServer() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.server != nil && r.server.Running() {
		return nil
	}
	if strings.TrimSpace(r.source) == "" {
		return errors.New("no mappings file or folder is configured")
	}

	store := mock.NewStore(r.source)
	if err := store.Load(); err != nil {
		r.store = store // retained so lastError is visible
		return err
	}
	store.OnReload(func(_ *mock.Ruleset, err error) {
		if err != nil {
			r.emitLog("warn", "reload failed: "+err.Error())
		}
		r.pushState()
	})
	store.StartWatching(500 * time.Millisecond)

	srv := mock.NewServer(r.host, r.port, store, r.journal)
	srv.SetLogger(func(format string, args ...any) {
		r.emitLog("info", fmt.Sprintf(format, args...))
	})
	srv.OnRequest = r.emitRequest
	if err := srv.Start(); err != nil {
		store.StopWatching()
		return err
	}
	r.store = store
	r.server = srv
	return nil
}

func (r *Runner) stopServer() {
	r.mu.Lock()
	srv := r.server
	store := r.store
	r.server = nil
	r.mu.Unlock()

	if srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = srv.Stop(ctx)
		cancel()
	}
	if store != nil {
		store.StopWatching()
	}
}

func (r *Runner) reload() error {
	r.mu.Lock()
	store := r.store
	r.mu.Unlock()
	if store == nil {
		return errors.New("server is not running")
	}
	return store.Reload()
}

func (r *Runner) stateMap() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()

	running := r.server != nil && r.server.Running()
	mappingCount := 0
	loadedAt := ""
	var stubs []mock.StubSummary
	lastErr := ""
	if r.store != nil {
		if rs := r.store.Current(); rs != nil {
			mappingCount = len(rs.Stubs)
			loadedAt = rs.LoadedAt.UTC().Format(time.RFC3339)
			stubs = rs.Summaries()
		}
		lastErr = r.store.LastError()
	}
	total, unmatched := r.journal.Counts()

	return map[string]any{
		"running":        running,
		"host":           r.host,
		"port":           r.port,
		"source":         r.source,
		"mappingCount":   mappingCount,
		"requestCount":   total,
		"unmatchedCount": unmatched,
		"lastError":      lastErr,
		"loadedAt":       loadedAt,
		"stubs":          stubs,
	}
}

func (r *Runner) pushState() {
	st := r.stateMap()
	st["ev"] = "state"
	r.emit(st)
}

func (r *Runner) emitRequest(e mock.JournalEntry) {
	r.emit(map[string]any{
		"ev":         "request",
		"seq":        e.Seq,
		"ts":         e.Time.UTC().Format(time.RFC3339Nano),
		"method":     e.Method,
		"url":        e.URL,
		"status":     e.Status,
		"matched":    e.Matched,
		"remoteAddr": e.RemoteAddr,
		"durationMs": e.DurationMs,
	})
}

func (r *Runner) emitLog(level, msg string) {
	r.emit(map[string]any{"ev": "log", "level": level, "msg": msg})
}

func (r *Runner) emit(v any) {
	r.encMu.Lock()
	defer r.encMu.Unlock()
	_ = r.enc.Encode(v)
}

func (r *Runner) reply(id float64, fields map[string]any) {
	m := map[string]any{"ev": "reply", "id": id}
	for k, v := range fields {
		m[k] = v
	}
	r.emit(m)
}

func (r *Runner) replyErr(id float64, err error) {
	if err != nil {
		r.reply(id, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	r.reply(id, map[string]any{"ok": true})
}
