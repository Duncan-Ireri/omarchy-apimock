package mock

import (
	"encoding/json"
	"testing"
)

func mustJSON(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("bad test JSON %q: %v", s, err)
	}
	return v
}

func TestJSONEqual(t *testing.T) {
	tests := []struct {
		name                  string
		expected, actual      string
		ignoreOrder, ignoreEx bool
		want                  bool
	}{
		{"identical objects", `{"a":1,"b":"x"}`, `{"b":"x","a":1}`, false, false, true},
		{"extra key rejected", `{"a":1}`, `{"a":1,"b":2}`, false, false, false},
		{"extra key allowed", `{"a":1}`, `{"a":1,"b":2}`, false, true, true},
		{"nested mismatch", `{"a":{"b":1}}`, `{"a":{"b":2}}`, false, false, false},
		{"array order matters", `[1,2,3]`, `[3,2,1]`, false, false, false},
		{"array order ignored", `[1,2,3]`, `[3,2,1]`, true, false, true},
		{"array multiset diff len", `[1,2]`, `[1,2,3]`, true, false, false},
		{"number int vs float", `{"a":1}`, `{"a":1.0}`, false, false, true},
		{"null match", `{"a":null}`, `{"a":null}`, false, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := jsonEqual(mustJSON(t, tt.expected), mustJSON(t, tt.actual), tt.ignoreOrder, tt.ignoreEx)
			if got != tt.want {
				t.Fatalf("jsonEqual = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestJSONPathQuery(t *testing.T) {
	doc := mustJSON(t, `{"user":{"name":"Ada","roles":["admin","dev"]},"count":3,"active":true}`)
	tests := []struct {
		path      string
		wantFound bool
		wantVal   any
	}{
		{"$.user.name", true, "Ada"},
		{"$.user.roles[0]", true, "admin"},
		{"$['user']['roles'][1]", true, "dev"},
		{"$.count", true, float64(3)},
		{"$.active", true, true},
		{"$.user.missing", false, nil},
		{"$.user.roles[9]", false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			v, ok := jsonPathQuery(tt.path, doc)
			if ok != tt.wantFound {
				t.Fatalf("found = %v, want %v", ok, tt.wantFound)
			}
			if ok && v != tt.wantVal {
				t.Fatalf("value = %#v, want %#v", v, tt.wantVal)
			}
		})
	}
}

func TestJSONPathInvalid(t *testing.T) {
	for _, p := range []string{"user.name", "$..name", "$.a[-1]", "$.a["} {
		if _, err := tokenizeJSONPath(p); err == nil {
			t.Fatalf("expected error for %q", p)
		}
	}
}
