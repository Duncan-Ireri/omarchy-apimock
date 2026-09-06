package mock

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// matchInput is everything about an incoming request the matcher looks at.
type matchInput struct {
	Method  string
	Path    string
	FullURL string // path + "?" + rawQuery
	Query   url.Values
	Header  http.Header
	Cookies map[string]string
	Body    []byte
}

func newMatchInput(r *http.Request, body []byte) *matchInput {
	full := r.URL.Path
	if r.URL.RawQuery != "" {
		full += "?" + r.URL.RawQuery
	}
	cookies := map[string]string{}
	for _, c := range r.Cookies() {
		cookies[c.Name] = c.Value
	}
	return &matchInput{
		Method:  strings.ToUpper(r.Method),
		Path:    r.URL.Path,
		FullURL: full,
		Query:   r.URL.Query(),
		Header:  r.Header,
		Cookies: cookies,
		Body:    body,
	}
}

// match reports whether the request criteria are satisfied. On failure it also
// returns a short reason, used for the not-matched diagnostics page.
func (r *Request) match(in *matchInput) (bool, string) {
	if m := r.method(); m != "ANY" && m != in.Method {
		return false, fmt.Sprintf("method is %s, wanted %s", in.Method, m)
	}
	if ok, reason := r.matchURL(in); !ok {
		return false, reason
	}
	for name, matcher := range r.QueryParameters {
		vals := in.Query[name]
		if ok, reason := matcher.matchValues(vals); !ok {
			return false, fmt.Sprintf("query %q %s", name, reason)
		}
	}
	for name, matcher := range r.Headers {
		vals := headerValues(in.Header, name)
		if ok, reason := matcher.matchValues(vals); !ok {
			return false, fmt.Sprintf("header %q %s", name, reason)
		}
	}
	for name, matcher := range r.Cookies {
		var vals []string
		if v, ok := in.Cookies[name]; ok {
			vals = []string{v}
		}
		if ok, reason := matcher.matchValues(vals); !ok {
			return false, fmt.Sprintf("cookie %q %s", name, reason)
		}
	}
	for i := range r.BodyPatterns {
		if ok, reason := r.BodyPatterns[i].matchOne(string(in.Body), true); !ok {
			return false, fmt.Sprintf("body pattern %d %s", i, reason)
		}
	}
	return true, ""
}

func (r *Request) matchURL(in *matchInput) (bool, string) {
	switch {
	case r.URLPath != "":
		if in.Path != r.URLPath {
			return false, fmt.Sprintf("path %q != %q", in.Path, r.URLPath)
		}
	case r.compiled.urlPathPattern != nil:
		if !r.compiled.urlPathPattern.MatchString(in.Path) {
			return false, fmt.Sprintf("path %q does not match /%s/", in.Path, r.URLPathPattern)
		}
	case r.URL != "":
		if in.FullURL != r.URL {
			return false, fmt.Sprintf("url %q != %q", in.FullURL, r.URL)
		}
	case r.compiled.urlPattern != nil:
		if !r.compiled.urlPattern.MatchString(in.FullURL) {
			return false, fmt.Sprintf("url %q does not match /%s/", in.FullURL, r.URLPattern)
		}
	}
	return true, ""
}

func headerValues(h http.Header, name string) []string {
	// http.Header is already canonicalised, but be forgiving about the name the
	// stub author wrote.
	if v, ok := h[http.CanonicalHeaderKey(name)]; ok {
		return v
	}
	for k, v := range h {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return nil
}

// matchValues applies the matcher to a header/query field that may carry zero or
// more values. Presence-based conditions look at the whole set; value conditions
// pass if any single value satisfies them.
func (m *Matcher) matchValues(vals []string) (bool, string) {
	if m.Absent != nil {
		if *m.Absent && len(vals) > 0 {
			return false, "should be absent"
		}
		if !*m.Absent && len(vals) == 0 {
			return false, "should be present"
		}
		if *m.Absent {
			return true, ""
		}
	}
	if len(vals) == 0 {
		if m.onlyAbsent() {
			return true, ""
		}
		return false, "is absent"
	}
	var last string
	for _, v := range vals {
		if ok, reason := m.matchOne(v, true); ok {
			return true, ""
		} else {
			last = reason
		}
	}
	return false, last
}

func (m *Matcher) onlyAbsent() bool {
	return m.EqualTo == nil && m.Contains == nil && m.Matches == nil &&
		m.DoesNotMatch == nil && !m.equalToJsonSet && !m.jsonPathSet
}

// matchOne applies every configured condition (AND) to a single string value.
func (m *Matcher) matchOne(value string, present bool) (bool, string) {
	if m.Absent != nil && *m.Absent {
		if present {
			return false, "should be absent"
		}
		return true, ""
	}
	if m.EqualTo != nil {
		if m.CaseInsensitive {
			if !strings.EqualFold(value, *m.EqualTo) {
				return false, fmt.Sprintf("!= %q (ci)", *m.EqualTo)
			}
		} else if value != *m.EqualTo {
			return false, fmt.Sprintf("!= %q", *m.EqualTo)
		}
	}
	if m.Contains != nil {
		hay, needle := value, *m.Contains
		if m.CaseInsensitive {
			hay, needle = strings.ToLower(hay), strings.ToLower(needle)
		}
		if !strings.Contains(hay, needle) {
			return false, fmt.Sprintf("does not contain %q", *m.Contains)
		}
	}
	if m.matches != nil && !m.matches.MatchString(value) {
		return false, fmt.Sprintf("does not match /%s/", *m.Matches)
	}
	if m.doesNotMatch != nil && m.doesNotMatch.MatchString(value) {
		return false, fmt.Sprintf("matches /%s/ but should not", *m.DoesNotMatch)
	}
	if m.equalToJsonSet {
		var got any
		if err := json.Unmarshal([]byte(value), &got); err != nil {
			return false, "is not valid JSON"
		}
		if !jsonEqual(m.equalToJsonVal, got, m.IgnoreArrayOrder, m.IgnoreExtraElements) {
			return false, "JSON does not equal expected"
		}
	}
	if m.jsonPathSet {
		var doc any
		if err := json.Unmarshal([]byte(value), &doc); err != nil {
			return false, "is not valid JSON"
		}
		located, ok := jsonPathQuery(m.jsonPathExpr, doc)
		if !ok {
			return false, fmt.Sprintf("JSONPath %s not found", m.jsonPathExpr)
		}
		if m.jsonPathEqualTo != nil {
			s, ok := jsonScalarString(located)
			if !ok || s != *m.jsonPathEqualTo {
				return false, fmt.Sprintf("JSONPath %s != %q", m.jsonPathExpr, *m.jsonPathEqualTo)
			}
		}
	}
	return true, ""
}
