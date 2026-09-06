package mock

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Stub is one request->response rule, a WireMock-compatible subset.
type Stub struct {
	Name             string            `json:"name"`
	Priority         *int              `json:"priority"`
	Request          Request           `json:"request"`
	Response         Response          `json:"response"`
	PostServeActions []PostServeAction `json:"postServeActions"`

	// Filled in during loading, not from JSON.
	SourceFile string `json:"-"`
	Index      int    `json:"-"`
}

func (s *Stub) priority() int {
	if s.Priority == nil {
		return 5
	}
	return *s.Priority
}

// label is a stable human name for logs and the not-matched page.
func (s *Stub) label() string {
	if s.Name != "" {
		return s.Name
	}
	r := s.Request
	path := r.URLPath
	if path == "" {
		path = r.URLPathPattern
	}
	if path == "" {
		path = r.URL
	}
	if path == "" {
		path = r.URLPattern
	}
	if path == "" {
		path = "(any url)"
	}
	return strings.TrimSpace(r.method() + " " + path)
}

// Request is the matching side of a stub.
type Request struct {
	Method          string             `json:"method"`
	URL             string             `json:"url"`
	URLPath         string             `json:"urlPath"`
	URLPathPattern  string             `json:"urlPathPattern"`
	URLPattern      string             `json:"urlPattern"`
	QueryParameters map[string]Matcher `json:"queryParameters"`
	Headers         map[string]Matcher `json:"headers"`
	Cookies         map[string]Matcher `json:"cookies"`
	BodyPatterns    []Matcher          `json:"bodyPatterns"`

	compiled compiledRequest
}

func (r *Request) method() string {
	if r.Method == "" {
		return "ANY"
	}
	return strings.ToUpper(r.Method)
}

type compiledRequest struct {
	urlPathPattern *regexp.Regexp
	urlPattern     *regexp.Regexp
}

// Matcher is WireMock's value-match object, reused for query params, headers,
// cookies and body patterns.
type Matcher struct {
	EqualTo         *string `json:"equalTo"`
	CaseInsensitive bool    `json:"caseInsensitive"`
	Contains        *string `json:"contains"`
	Matches         *string `json:"matches"`
	DoesNotMatch    *string `json:"doesNotMatch"`
	Absent          *bool   `json:"absent"`

	EqualToJson         json.RawMessage `json:"equalToJson"`
	IgnoreArrayOrder    bool            `json:"ignoreArrayOrder"`
	IgnoreExtraElements bool            `json:"ignoreExtraElements"`

	MatchesJsonPath json.RawMessage `json:"matchesJsonPath"`

	// compiled
	matches         *regexp.Regexp
	doesNotMatch    *regexp.Regexp
	equalToJsonVal  any
	equalToJsonSet  bool
	jsonPathExpr    string
	jsonPathEqualTo *string
	jsonPathSet     bool
}

// Response is the reply side of a stub.
type Response struct {
	Status        int             `json:"status"`
	StatusMessage string          `json:"statusMessage"`
	Headers       map[string]any  `json:"headers"`
	Body          *string         `json:"body"`
	JsonBody      json.RawMessage `json:"jsonBody"`
	Base64Body    *string         `json:"base64Body"`
	BodyFileName  string          `json:"bodyFileName"`
	FixedDelayMs  int             `json:"fixedDelayMilliseconds"`
}

func (r *Response) status() int {
	if r.Status == 0 {
		return 200
	}
	return r.Status
}

// PostServeAction fires after the response is written. Both the WireMock 3
// shape ({name:"webhook", parameters:{...}}) and the shorthand ({webhook:{...}})
// are accepted.
type PostServeAction struct {
	Name       string       `json:"name"`
	Parameters *WebhookSpec `json:"parameters"`
	Webhook    *WebhookSpec `json:"webhook"`
}

func (a *PostServeAction) spec() *WebhookSpec {
	if a.Webhook != nil {
		return a.Webhook
	}
	if a.Parameters != nil && (a.Name == "" || strings.EqualFold(a.Name, "webhook")) {
		return a.Parameters
	}
	return nil
}

// WebhookSpec is an outbound HTTP call.
type WebhookSpec struct {
	Method   string          `json:"method"`
	URL      string          `json:"url"`
	Headers  map[string]any  `json:"headers"`
	Body     *string         `json:"body"`
	JsonBody json.RawMessage `json:"jsonBody"`
	DelayMs  int             `json:"delayMilliseconds"`
}

// mappingFile is the top-level document shape.
type mappingFile struct {
	Mappings []*Stub `json:"mappings"`
}

// decodeStubs accepts {mappings:[...]}, a bare [...] array, or a single {...}.
func decodeStubs(raw []byte) ([]*Stub, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return nil, nil
	}
	switch trimmed[0] {
	case '[':
		var stubs []*Stub
		if err := strictUnmarshal(raw, &stubs); err != nil {
			return nil, err
		}
		return stubs, nil
	case '{':
		// Could be {mappings:[...]} or a single stub. Peek at the keys.
		var probe map[string]json.RawMessage
		if err := json.Unmarshal(raw, &probe); err != nil {
			return nil, err
		}
		if _, ok := probe["mappings"]; ok {
			var mf mappingFile
			if err := strictUnmarshal(raw, &mf); err != nil {
				return nil, err
			}
			return mf.Mappings, nil
		}
		var s Stub
		if err := strictUnmarshal(raw, &s); err != nil {
			return nil, err
		}
		return []*Stub{&s}, nil
	default:
		return nil, fmt.Errorf("expected a JSON object or array, got %q", trimmed[:1])
	}
}

func strictUnmarshal(raw []byte, v any) error {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		// Fall back to lenient decoding: unknown fields are a warning, not fatal,
		// so that newer WireMock files still load on older builds.
		if strings.Contains(err.Error(), "unknown field") {
			return json.Unmarshal(raw, v)
		}
		return err
	}
	return nil
}

// compile validates and precompiles every stub. It returns the first error.
func compile(stubs []*Stub) error {
	for _, s := range stubs {
		if err := s.Request.compile(); err != nil {
			return fmt.Errorf("stub %q: %w", s.label(), err)
		}
		if err := s.Response.validate(); err != nil {
			return fmt.Errorf("stub %q: %w", s.label(), err)
		}
	}
	return nil
}

func (r *Request) compile() error {
	if r.URLPathPattern != "" {
		re, err := regexp.Compile(r.URLPathPattern)
		if err != nil {
			return fmt.Errorf("urlPathPattern: %w", err)
		}
		r.compiled.urlPathPattern = re
	}
	if r.URLPattern != "" {
		re, err := regexp.Compile(r.URLPattern)
		if err != nil {
			return fmt.Errorf("urlPattern: %w", err)
		}
		r.compiled.urlPattern = re
	}
	for k, m := range r.QueryParameters {
		if err := m.compile(); err != nil {
			return fmt.Errorf("queryParameters[%q]: %w", k, err)
		}
		r.QueryParameters[k] = m
	}
	for k, m := range r.Headers {
		if err := m.compile(); err != nil {
			return fmt.Errorf("headers[%q]: %w", k, err)
		}
		r.Headers[k] = m
	}
	for k, m := range r.Cookies {
		if err := m.compile(); err != nil {
			return fmt.Errorf("cookies[%q]: %w", k, err)
		}
		r.Cookies[k] = m
	}
	for i := range r.BodyPatterns {
		if err := r.BodyPatterns[i].compile(); err != nil {
			return fmt.Errorf("bodyPatterns[%d]: %w", i, err)
		}
	}
	return nil
}

func (m *Matcher) compile() error {
	if m.Matches != nil {
		re, err := regexp.Compile(*m.Matches)
		if err != nil {
			return fmt.Errorf("matches: %w", err)
		}
		m.matches = re
	}
	if m.DoesNotMatch != nil {
		re, err := regexp.Compile(*m.DoesNotMatch)
		if err != nil {
			return fmt.Errorf("doesNotMatch: %w", err)
		}
		m.doesNotMatch = re
	}
	if len(m.EqualToJson) > 0 {
		v, err := decodeMaybeStringJSON(m.EqualToJson)
		if err != nil {
			return fmt.Errorf("equalToJson: %w", err)
		}
		m.equalToJsonVal = v
		m.equalToJsonSet = true
	}
	if len(m.MatchesJsonPath) > 0 {
		expr, eq, err := decodeJsonPathSpec(m.MatchesJsonPath)
		if err != nil {
			return fmt.Errorf("matchesJsonPath: %w", err)
		}
		m.jsonPathExpr = expr
		m.jsonPathEqualTo = eq
		m.jsonPathSet = true
	}
	if m.empty() {
		return fmt.Errorf("matcher has no condition")
	}
	return nil
}

func (m *Matcher) empty() bool {
	return m.EqualTo == nil && m.Contains == nil && m.Matches == nil &&
		m.DoesNotMatch == nil && m.Absent == nil && !m.equalToJsonSet && !m.jsonPathSet
}

// decodeMaybeStringJSON accepts either a JSON value or a JSON string that itself
// contains JSON (WireMock allows both spellings of equalToJson).
func decodeMaybeStringJSON(raw json.RawMessage) (any, error) {
	t := strings.TrimSpace(string(raw))
	if len(t) > 0 && t[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, err
		}
		var v any
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			return nil, err
		}
		return v, nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return v, nil
}

func decodeJsonPathSpec(raw json.RawMessage) (expr string, equalTo *string, err error) {
	t := strings.TrimSpace(string(raw))
	if len(t) > 0 && t[0] == '"' {
		if err = json.Unmarshal(raw, &expr); err != nil {
			return "", nil, err
		}
		return expr, nil, nil
	}
	var obj struct {
		Expression string  `json:"expression"`
		EqualTo    *string `json:"equalTo"`
		Contains   *string `json:"contains"`
	}
	if err = json.Unmarshal(raw, &obj); err != nil {
		return "", nil, err
	}
	if obj.Expression == "" {
		return "", nil, fmt.Errorf("object form needs an \"expression\"")
	}
	if obj.EqualTo != nil {
		return obj.Expression, obj.EqualTo, nil
	}
	if obj.Contains != nil {
		return obj.Expression, obj.Contains, nil
	}
	return obj.Expression, nil, nil
}

func (r *Response) validate() error {
	bodies := 0
	for _, present := range []bool{r.Body != nil, len(r.JsonBody) > 0, r.Base64Body != nil, r.BodyFileName != ""} {
		if present {
			bodies++
		}
	}
	if bodies > 1 {
		return fmt.Errorf("response sets more than one of body/jsonBody/base64Body/bodyFileName")
	}
	if r.BodyFileName != "" {
		if strings.HasPrefix(r.BodyFileName, "/") || strings.Contains(r.BodyFileName, "..") {
			return fmt.Errorf("bodyFileName %q must be a relative path without \"..\"", r.BodyFileName)
		}
	}
	if r.Status != 0 && (r.Status < 100 || r.Status > 599) {
		return fmt.Errorf("status %d is out of range", r.Status)
	}
	return nil
}

// normalizeHeaders turns a WireMock header map (string or []string values) into
// canonical form.
func normalizeHeaders(in map[string]any) map[string][]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]string, len(in))
	for k, v := range in {
		switch t := v.(type) {
		case string:
			out[k] = []string{t}
		case []any:
			for _, e := range t {
				out[k] = append(out[k], fmt.Sprint(e))
			}
		case float64:
			out[k] = []string{strconv.FormatFloat(t, 'f', -1, 64)}
		case bool:
			out[k] = []string{strconv.FormatBool(t)}
		default:
			out[k] = []string{fmt.Sprint(t)}
		}
	}
	return out
}
