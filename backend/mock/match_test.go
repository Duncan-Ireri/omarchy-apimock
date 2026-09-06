package mock

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func compiledStub(t *testing.T, body string) *Stub {
	t.Helper()
	stubs, err := decodeStubs([]byte(body))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if err := compile(stubs); err != nil {
		t.Fatalf("compile: %v", err)
	}
	return stubs[0]
}

func TestRequestMatch(t *testing.T) {
	tests := []struct {
		name   string
		stub   string
		method string
		target string
		body   string
		hdr    map[string]string
		want   bool
	}{
		{
			name:   "method and exact path",
			stub:   `{"request":{"method":"GET","urlPath":"/pets"},"response":{"status":200}}`,
			method: "GET", target: "/pets", want: true,
		},
		{
			name:   "wrong method",
			stub:   `{"request":{"method":"GET","urlPath":"/pets"},"response":{"status":200}}`,
			method: "POST", target: "/pets", want: false,
		},
		{
			name:   "path pattern",
			stub:   `{"request":{"method":"GET","urlPathPattern":"/pets/[0-9]+"},"response":{"status":200}}`,
			method: "GET", target: "/pets/42", want: true,
		},
		{
			name:   "path pattern miss",
			stub:   `{"request":{"method":"GET","urlPathPattern":"/pets/[0-9]+"},"response":{"status":200}}`,
			method: "GET", target: "/pets/abc", want: false,
		},
		{
			name:   "query equalTo",
			stub:   `{"request":{"urlPath":"/pets","queryParameters":{"status":{"equalTo":"sold"}}},"response":{"status":200}}`,
			method: "GET", target: "/pets?status=sold", want: true,
		},
		{
			name:   "query absent required",
			stub:   `{"request":{"urlPath":"/pets","queryParameters":{"status":{"absent":true}}},"response":{"status":200}}`,
			method: "GET", target: "/pets?status=sold", want: false,
		},
		{
			name:   "header contains ci",
			stub:   `{"request":{"urlPath":"/x","headers":{"Content-Type":{"contains":"JSON","caseInsensitive":true}}},"response":{"status":200}}`,
			method: "GET", target: "/x", hdr: map[string]string{"Content-Type": "application/json"}, want: true,
		},
		{
			name:   "body equalToJson ignoreExtra",
			stub:   `{"request":{"urlPath":"/x","bodyPatterns":[{"equalToJson":{"a":1},"ignoreExtraElements":true}]},"response":{"status":200}}`,
			method: "POST", target: "/x", body: `{"a":1,"b":2}`, want: true,
		},
		{
			name:   "body jsonpath equalTo",
			stub:   `{"request":{"urlPath":"/x","bodyPatterns":[{"matchesJsonPath":{"expression":"$.name","equalTo":"Rex"}}]},"response":{"status":200}}`,
			method: "POST", target: "/x", body: `{"name":"Rex"}`, want: true,
		},
		{
			name:   "body jsonpath equalTo miss",
			stub:   `{"request":{"urlPath":"/x","bodyPatterns":[{"matchesJsonPath":{"expression":"$.name","equalTo":"Rex"}}]},"response":{"status":200}}`,
			method: "POST", target: "/x", body: `{"name":"Fido"}`, want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := compiledStub(t, tt.stub)
			r := httptest.NewRequest(tt.method, tt.target, strings.NewReader(tt.body))
			for k, v := range tt.hdr {
				r.Header.Set(k, v)
			}
			in := newMatchInput(r, []byte(tt.body))
			got, reason := stub.Request.match(in)
			if got != tt.want {
				t.Fatalf("match = %v (%s), want %v", got, reason, tt.want)
			}
		})
	}
}
