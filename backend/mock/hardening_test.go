package mock

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestFireWebhooksBoundsConcurrency proves a single matched request cannot spawn
// an unbounded number of webhook goroutines: the semaphore admits at most its
// capacity and the rest are dropped.
func TestFireWebhooksBoundsConcurrency(t *testing.T) {
	var inFlight, total int64
	var mu sync.Mutex
	var maxSeen int64

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt64(&total, 1)
		n := atomic.AddInt64(&inFlight, 1)
		mu.Lock()
		if n > maxSeen {
			maxSeen = n
		}
		mu.Unlock()
		time.Sleep(50 * time.Millisecond)
		atomic.AddInt64(&inFlight, -1)
		w.WriteHeader(http.StatusOK)
	}))
	defer target.Close()

	const capacity = 3
	s := &Server{logf: func(string, ...any) {}, webhookSem: make(chan struct{}, capacity)}

	stub := &Stub{}
	for i := 0; i < 20; i++ {
		stub.PostServeActions = append(stub.PostServeActions, PostServeAction{
			Webhook: &WebhookSpec{Method: "POST", URL: target.URL},
		})
	}

	s.fireWebhooks(stub)
	s.webhookWG.Wait()

	if maxSeen > capacity {
		t.Fatalf("webhook concurrency reached %d, cap is %d", maxSeen, capacity)
	}
	if got := atomic.LoadInt64(&total); got != capacity {
		t.Fatalf("expected %d webhooks to fire and the rest dropped, got %d", capacity, got)
	}
}

// TestStartSetsServerDeadlines guards against a regression where the mock server
// is started without full request/response/idle timeouts.
func TestStartSetsServerDeadlines(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "m.json")
	writeFile(t, f, `{"mappings":[{"request":{"urlPath":"/x"},"response":{"status":200}}]}`)

	store := NewStore(f)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	s := NewServer("127.0.0.1", 0, store, NewJournal(10))
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	switch {
	case s.httpSrv.ReadTimeout == 0:
		t.Error("ReadTimeout not set")
	case s.httpSrv.WriteTimeout == 0:
		t.Error("WriteTimeout not set")
	case s.httpSrv.IdleTimeout == 0:
		t.Error("IdleTimeout not set")
	case s.httpSrv.ReadHeaderTimeout == 0:
		t.Error("ReadHeaderTimeout not set")
	}
}
