package mock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// maxConcurrentWebhooks caps the webhook goroutines in flight at once. Each
// served request can queue post-serve webhooks; without a ceiling a burst of
// traffic (more likely once the bind address is not loopback) spawns unbounded
// goroutines and outbound sockets. Webhooks past the cap are dropped and logged.
const maxConcurrentWebhooks = 32

// Server is a running mock HTTP endpoint backed by a Store.
type Server struct {
	Addr    string
	store   *Store
	journal *Journal
	logf    func(format string, args ...any)

	httpSrv *http.Server
	ln      net.Listener

	mu      sync.Mutex
	running bool

	// wg tracks in-flight webhook goroutines so Stop can wait for them; sem
	// bounds how many run at once (see maxConcurrentWebhooks).
	webhookCtx    context.Context
	webhookCancel context.CancelFunc
	webhookWG     sync.WaitGroup
	webhookSem    chan struct{}

	// OnRequest, if set, is called for every served request (for live streaming).
	OnRequest func(JournalEntry)
}

// NewServer prepares (does not start) a server.
func NewServer(host string, port int, store *Store, journal *Journal) *Server {
	if host == "" {
		host = "127.0.0.1"
	}
	return &Server{
		Addr:       fmt.Sprintf("%s:%d", host, port),
		store:      store,
		journal:    journal,
		logf:       func(string, ...any) {},
		webhookSem: make(chan struct{}, maxConcurrentWebhooks),
	}
}

func (s *Server) SetLogger(fn func(format string, args ...any)) {
	if fn != nil {
		s.logf = fn
	}
}

func (s *Server) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// Start binds the port and begins serving. It returns once the listener is open
// (or fails to bind).
func (s *Server) Start() error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	ln, err := net.Listen("tcp", s.Addr)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	s.ln = ln
	s.webhookCtx, s.webhookCancel = context.WithCancel(context.Background())
	s.httpSrv = &http.Server{
		Handler:           http.HandlerFunc(s.handle),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	s.running = true
	s.mu.Unlock()

	go func() {
		if err := s.httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.logf("serve error: %v", err)
		}
	}()
	return nil
}

// Stop gracefully shuts the server down and waits for outstanding webhooks.
func (s *Server) Stop(ctx context.Context) error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	srv := s.httpSrv
	cancel := s.webhookCancel
	s.mu.Unlock()

	err := srv.Shutdown(ctx)
	if cancel != nil {
		cancel()
	}
	s.webhookWG.Wait()
	return err
}

func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	body, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	_ = r.Body.Close()

	rs := s.store.Current()
	in := newMatchInput(r, body)

	var matched *Stub
	var misses []string
	if rs != nil {
		for _, stub := range rs.Stubs {
			ok, reason := stub.Request.match(in)
			if ok {
				matched = stub
				break
			}
			if reason != "" && len(misses) < 5 {
				misses = append(misses, stub.label()+": "+reason)
			}
		}
	}

	var status int
	baseDir := ""
	if rs != nil {
		baseDir = rs.BaseDir
	}

	if matched != nil {
		status, _ = matched.Response.render(w, baseDir)
		s.fireWebhooks(matched)
	} else {
		status = http.StatusNotFound
		writeNotMatched(w, in, misses, rs == nil, s.store.LastError())
	}

	entry := JournalEntry{
		Time:       start,
		Method:     in.Method,
		URL:        in.FullURL,
		Status:     status,
		RemoteAddr: clientIP(r),
		DurationMs: time.Since(start).Milliseconds(),
	}
	if matched != nil {
		entry.Matched = matched.label()
	}
	entry = s.journal.Record(entry)
	if s.OnRequest != nil {
		s.OnRequest(entry)
	}
	s.logf("%s %s -> %d (%s)", entry.Method, entry.URL, status, matchWord(matched))
}

func (s *Server) fireWebhooks(stub *Stub) {
	ctx := s.webhookCtx
	if ctx == nil { // constructed without Start (tests); no lifecycle to cancel
		ctx = context.Background()
	}
	for _, action := range stub.PostServeActions {
		spec := action.spec()
		if spec == nil || spec.URL == "" {
			continue
		}
		select {
		case s.webhookSem <- struct{}{}:
		default:
			s.logf("webhook %s %s dropped: %d already in flight",
				strings.ToUpper(spec.Method), spec.URL, cap(s.webhookSem))
			continue
		}
		s.webhookWG.Add(1)
		go func() {
			defer s.webhookWG.Done()
			defer func() { <-s.webhookSem }()
			res := spec.fire(ctx)
			if res.Err != nil {
				s.logf("webhook %s %s failed: %v", res.Method, res.URL, res.Err)
			} else {
				s.logf("webhook %s %s -> %d", res.Method, res.URL, res.Status)
			}
		}()
	}
}

type notMatchedBody struct {
	Error        string   `json:"error"`
	Method       string   `json:"method"`
	URL          string   `json:"url"`
	ClosestStubs []string `json:"closestStubs,omitempty"`
	LoadError    string   `json:"loadError,omitempty"`
}

func writeNotMatched(w http.ResponseWriter, in *matchInput, misses []string, noRuleset bool, loadErr string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Apimock-Unmatched", "true")
	w.WriteHeader(http.StatusNotFound)
	msg := "No stub matched this request"
	if noRuleset {
		msg = "No mappings are loaded"
	}
	_ = json.NewEncoder(w).Encode(notMatchedBody{
		Error:        msg,
		Method:       in.Method,
		URL:          in.FullURL,
		ClosestStubs: misses,
		LoadError:    loadErr,
	})
}

func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func matchWord(s *Stub) string {
	if s == nil {
		return "unmatched"
	}
	return s.label()
}
