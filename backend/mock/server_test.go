package mock

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// testServer wires a Store + Journal to an httptest.Server using the real handler.
func testServer(t *testing.T, source string) (*httptest.Server, *Journal) {
	t.Helper()
	store := NewStore(source)
	if err := store.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	j := NewJournal(50)
	s := &Server{store: store, journal: j, logf: func(string, ...any) {}}
	ts := httptest.NewServer(http.HandlerFunc(s.handle))
	t.Cleanup(ts.Close)
	return ts, j
}

func TestServeExamplePetstore(t *testing.T) {
	root, _ := filepath.Abs("../../examples/petstore.json")
	ts, j := testServer(t, root)

	t.Run("get by id", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/pets/5")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("status %d", resp.StatusCode)
		}
		var v map[string]any
		json.NewDecoder(resp.Body).Decode(&v)
		if v["name"] != "Rex" {
			t.Fatalf("body %v", v)
		}
	})

	t.Run("query wins over generic by priority", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/pets?status=sold")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(b), "Biscuit") || strings.Contains(string(b), "Rex") {
			t.Fatalf("expected only sold list, got %s", b)
		}
	})

	t.Run("unmatched is 404 with diagnostics", func(t *testing.T) {
		resp, err := http.Get(ts.URL + "/nope")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Fatalf("status %d", resp.StatusCode)
		}
		if resp.Header.Get("X-Apimock-Unmatched") != "true" {
			t.Fatal("missing unmatched header")
		}
		var nm notMatchedBody
		json.NewDecoder(resp.Body).Decode(&nm)
		if nm.Method != "GET" || nm.URL != "/nope" {
			t.Fatalf("diagnostics %+v", nm)
		}
	})

	t.Run("post body match sets Location and delay", func(t *testing.T) {
		start := time.Now()
		resp, err := http.Post(ts.URL+"/pets", "application/json", strings.NewReader(`{"name":"Rex"}`))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 201 || resp.Header.Get("Location") != "/pets/99" {
			t.Fatalf("status %d loc %q", resp.StatusCode, resp.Header.Get("Location"))
		}
		if time.Since(start) < 40*time.Millisecond {
			t.Fatal("fixedDelayMilliseconds not applied")
		}
	})

	total, unmatched := j.Counts()
	if total != 4 || unmatched != 1 {
		t.Fatalf("journal counts total=%d unmatched=%d", total, unmatched)
	}
}

func TestHotReload(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "m.json")
	writeFile(t, f, `{"mappings":[{"request":{"urlPath":"/v"},"response":{"status":200,"body":"one"}}]}`)

	store := NewStore(f)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	store.StartWatching(20 * time.Millisecond)
	defer store.StopWatching()

	s := &Server{store: store, journal: NewJournal(10), logf: func(string, ...any) {}}
	ts := httptest.NewServer(http.HandlerFunc(s.handle))
	defer ts.Close()

	get := func() string {
		resp, err := http.Get(ts.URL + "/v")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}
	if got := get(); got != "one" {
		t.Fatalf("got %q", got)
	}

	time.Sleep(30 * time.Millisecond) // ensure mtime advances
	writeFile(t, f, `{"mappings":[{"request":{"urlPath":"/v"},"response":{"status":200,"body":"two"}}]}`)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if get() == "two" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("hot reload did not pick up the change")
}

func TestReloadKeepsGoodRulesetOnParseError(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "m.json")
	writeFile(t, f, `{"mappings":[{"request":{"urlPath":"/v"},"response":{"status":200,"body":"ok"}}]}`)

	store := NewStore(f)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	writeFile(t, f, `{ this is not json`)
	if err := store.Reload(); err == nil {
		t.Fatal("expected reload error")
	}
	if store.Current() == nil || len(store.Current().Stubs) != 1 {
		t.Fatal("previous good ruleset was dropped")
	}
	if store.LastError() == "" {
		t.Fatal("lastError not set")
	}
}
