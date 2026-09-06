package mock

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Ruleset is an immutable, ordered set of stubs plus the directory that
// bodyFileName paths resolve against.
type Ruleset struct {
	Stubs    []*Stub
	BaseDir  string
	Source   string
	LoadedAt time.Time
}

// StubSummary is a compact description for the plugin panel.
type StubSummary struct {
	Name     string `json:"name"`
	Method   string `json:"method"`
	URL      string `json:"url"`
	Priority int    `json:"priority"`
	Source   string `json:"source"`
}

func (rs *Ruleset) Summaries() []StubSummary {
	out := make([]StubSummary, 0, len(rs.Stubs))
	for _, s := range rs.Stubs {
		r := s.Request
		u := firstNonEmpty(r.URLPath, r.URLPathPattern, r.URL, r.URLPattern, "*")
		out = append(out, StubSummary{
			Name:     s.label(),
			Method:   r.method(),
			URL:      u,
			Priority: s.priority(),
			Source:   filepath.Base(s.SourceFile),
		})
	}
	return out
}

// LoadRuleset reads a single JSON file or a directory of *.json files.
func LoadRuleset(source string) (*Ruleset, error) {
	abs, err := filepath.Abs(expandUser(source))
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}

	var files []string
	var baseDir string
	if info.IsDir() {
		baseDir = abs
		entries, err := os.ReadDir(abs)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
				continue
			}
			files = append(files, filepath.Join(abs, e.Name()))
		}
		sort.Strings(files)
		if len(files) == 0 {
			return nil, fmt.Errorf("no .json mapping files in %s", abs)
		}
	} else {
		baseDir = filepath.Dir(abs)
		files = []string{abs}
	}

	var all []*Stub
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		stubs, err := decodeStubs(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(f), err)
		}
		for _, s := range stubs {
			if s == nil {
				continue
			}
			s.SourceFile = f
			s.Index = len(all)
			all = append(all, s)
		}
	}

	if err := compile(all); err != nil {
		return nil, err
	}

	// Stable sort by priority so the panel and the matcher agree on order.
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].priority() != all[j].priority() {
			return all[i].priority() < all[j].priority()
		}
		return all[i].Index < all[j].Index
	})

	return &Ruleset{Stubs: all, BaseDir: baseDir, Source: abs, LoadedAt: time.Now()}, nil
}

// Store holds the live ruleset and swaps it atomically. A background poller
// reloads when the source changes on disk; a parse failure keeps the previous
// good ruleset and is exposed via LastError.
type Store struct {
	source string

	mu        sync.RWMutex
	current   *Ruleset
	lastError string

	stamp    atomic.Int64 // newest mtime seen, unix nanos
	onReload func(*Ruleset, error)
	stopPoll chan struct{}
	pollWG   sync.WaitGroup
}

func NewStore(source string) *Store {
	return &Store{source: expandUser(source)}
}

func (s *Store) OnReload(fn func(*Ruleset, error)) { s.onReload = fn }

// Load performs the initial read. An error here is fatal to the caller's intent
// (there is nothing to serve yet).
func (s *Store) Load() error {
	rs, err := LoadRuleset(s.source)
	s.mu.Lock()
	if err != nil {
		s.lastError = err.Error()
	} else {
		s.current = rs
		s.lastError = ""
	}
	s.mu.Unlock()
	s.stamp.Store(s.newestMtime())
	if s.onReload != nil {
		s.onReload(rs, err)
	}
	return err
}

// Reload forces a re-read regardless of mtime.
func (s *Store) Reload() error {
	rs, err := LoadRuleset(s.source)
	s.mu.Lock()
	if err != nil {
		s.lastError = err.Error()
	} else {
		s.current = rs
		s.lastError = ""
	}
	s.mu.Unlock()
	s.stamp.Store(s.newestMtime())
	if s.onReload != nil {
		s.onReload(s.Current(), err)
	}
	return err
}

func (s *Store) Current() *Ruleset {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.current
}

func (s *Store) LastError() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastError
}

func (s *Store) StartWatching(interval time.Duration) {
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	s.stopPoll = make(chan struct{})
	s.pollWG.Add(1)
	go func() {
		defer s.pollWG.Done()
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-s.stopPoll:
				return
			case <-t.C:
				m := s.newestMtime()
				if m != 0 && m != s.stamp.Load() {
					s.stamp.Store(m)
					_ = s.Reload()
				}
			}
		}
	}()
}

func (s *Store) StopWatching() {
	if s.stopPoll != nil {
		close(s.stopPoll)
		s.pollWG.Wait()
		s.stopPoll = nil
	}
}

// newestMtime returns the most recent modification time under the source, as
// unix nanoseconds, or 0 if the source is gone.
func (s *Store) newestMtime() int64 {
	abs, err := filepath.Abs(expandUser(s.source))
	if err != nil {
		return 0
	}
	info, err := os.Stat(abs)
	if err != nil {
		return 0
	}
	if !info.IsDir() {
		return info.ModTime().UnixNano()
	}
	var newest int64 = info.ModTime().UnixNano()
	entries, err := os.ReadDir(abs)
	if err != nil {
		return newest
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		if fi, err := e.Info(); err == nil && fi.ModTime().UnixNano() > newest {
			newest = fi.ModTime().UnixNano()
		}
	}
	return newest
}

func expandUser(p string) string {
	if p == "~" || (len(p) >= 2 && p[:2] == "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			if p == "~" {
				return home
			}
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
