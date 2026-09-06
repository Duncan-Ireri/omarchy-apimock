package mock

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// jsonEqual reports whether actual matches expected. With ignoreExtraElements,
// objects in actual may carry keys expected does not mention. With
// ignoreArrayOrder, arrays match as multisets.
func jsonEqual(expected, actual any, ignoreArrayOrder, ignoreExtraElements bool) bool {
	switch exp := expected.(type) {
	case map[string]any:
		act, ok := actual.(map[string]any)
		if !ok {
			return false
		}
		if !ignoreExtraElements && len(act) != len(exp) {
			return false
		}
		for k, ev := range exp {
			av, ok := act[k]
			if !ok || !jsonEqual(ev, av, ignoreArrayOrder, ignoreExtraElements) {
				return false
			}
		}
		return true
	case []any:
		act, ok := actual.([]any)
		if !ok || len(act) != len(exp) {
			return false
		}
		if !ignoreArrayOrder {
			for i := range exp {
				if !jsonEqual(exp[i], act[i], ignoreArrayOrder, ignoreExtraElements) {
					return false
				}
			}
			return true
		}
		used := make([]bool, len(act))
		for _, ev := range exp {
			found := false
			for i, av := range act {
				if used[i] {
					continue
				}
				if jsonEqual(ev, av, ignoreArrayOrder, ignoreExtraElements) {
					used[i] = true
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	case float64:
		af, ok := toFloat(actual)
		return ok && af == exp
	case string:
		as, ok := actual.(string)
		return ok && as == exp
	case bool:
		ab, ok := actual.(bool)
		return ok && ab == exp
	case nil:
		return actual == nil
	default:
		return false
	}
}

func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	case int:
		return float64(t), true
	default:
		return 0, false
	}
}

// jsonPathQuery evaluates a small subset of JSONPath against v:
//
//	$              the document root
//	.key  ['key']  object member
//	[N]           array index (N >= 0)
//
// It returns the located value and whether the path resolved.
func jsonPathQuery(path string, v any) (any, bool) {
	toks, err := tokenizeJSONPath(path)
	if err != nil {
		return nil, false
	}
	cur := v
	for _, tok := range toks {
		switch {
		case tok.index >= 0:
			arr, ok := cur.([]any)
			if !ok || tok.index >= len(arr) {
				return nil, false
			}
			cur = arr[tok.index]
		default:
			obj, ok := cur.(map[string]any)
			if !ok {
				return nil, false
			}
			next, ok := obj[tok.key]
			if !ok {
				return nil, false
			}
			cur = next
		}
	}
	return cur, true
}

type jsonPathToken struct {
	key   string
	index int // -1 when this is an object-key token
}

func tokenizeJSONPath(path string) ([]jsonPathToken, error) {
	p := strings.TrimSpace(path)
	if !strings.HasPrefix(p, "$") {
		return nil, fmt.Errorf("JSONPath must start with $")
	}
	p = p[1:]
	var toks []jsonPathToken
	for len(p) > 0 {
		switch p[0] {
		case '.':
			p = p[1:]
			if strings.HasPrefix(p, ".") {
				return nil, fmt.Errorf("recursive descent (..) is not supported")
			}
			j := 0
			for j < len(p) && p[j] != '.' && p[j] != '[' {
				j++
			}
			if j == 0 {
				return nil, fmt.Errorf("empty member name")
			}
			toks = append(toks, jsonPathToken{key: p[:j], index: -1})
			p = p[j:]
		case '[':
			end := strings.IndexByte(p, ']')
			if end < 0 {
				return nil, fmt.Errorf("unclosed [")
			}
			inner := strings.TrimSpace(p[1:end])
			p = p[end+1:]
			if len(inner) >= 2 && (inner[0] == '\'' || inner[0] == '"') {
				toks = append(toks, jsonPathToken{key: inner[1 : len(inner)-1], index: -1})
				continue
			}
			n, err := strconv.Atoi(inner)
			if err != nil || n < 0 {
				return nil, fmt.Errorf("array index %q is not a non-negative integer", inner)
			}
			toks = append(toks, jsonPathToken{index: n})
		default:
			return nil, fmt.Errorf("unexpected %q in JSONPath", p[0])
		}
	}
	return toks, nil
}

// jsonScalarString renders a located JSONPath value for equality comparison the
// way WireMock does: strings as-is, numbers without trailing zeros, bools as
// true/false.
func jsonScalarString(v any) (string, bool) {
	switch t := v.(type) {
	case string:
		return t, true
	case bool:
		return strconv.FormatBool(t), true
	case float64:
		if t == math.Trunc(t) && !math.IsInf(t, 0) {
			return strconv.FormatInt(int64(t), 10), true
		}
		return strconv.FormatFloat(t, 'f', -1, 64), true
	case nil:
		return "null", true
	default:
		return "", false
	}
}
