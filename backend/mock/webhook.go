package mock

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// WebhookResult is reported back so the caller can log outbound calls.
type WebhookResult struct {
	URL    string
	Method string
	Status int
	Err    error
}

var webhookClient = &http.Client{Timeout: 15 * time.Second}

// fire executes one webhook. It is meant to run in its own goroutine, after the
// main response has been written.
func (spec *WebhookSpec) fire(ctx context.Context) WebhookResult {
	res := WebhookResult{URL: spec.URL, Method: strings.ToUpper(spec.Method)}
	if res.Method == "" {
		res.Method = http.MethodPost
	}
	if spec.DelayMs > 0 {
		select {
		case <-time.After(time.Duration(spec.DelayMs) * time.Millisecond):
		case <-ctx.Done():
			res.Err = ctx.Err()
			return res
		}
	}

	var bodyReader io.Reader
	var ctype string
	switch {
	case len(spec.JsonBody) > 0:
		var v any
		if err := json.Unmarshal(spec.JsonBody, &v); err != nil {
			res.Err = fmt.Errorf("jsonBody: %w", err)
			return res
		}
		b, _ := json.Marshal(v)
		bodyReader = bytes.NewReader(b)
		ctype = "application/json"
	case spec.Body != nil:
		bodyReader = strings.NewReader(*spec.Body)
	}

	req, err := http.NewRequestWithContext(ctx, res.Method, spec.URL, bodyReader)
	if err != nil {
		res.Err = err
		return res
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	for name, vals := range normalizeHeaders(spec.Headers) {
		for _, v := range vals {
			req.Header.Add(name, v)
		}
	}

	resp, err := webhookClient.Do(req)
	if err != nil {
		res.Err = err
		return res
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	res.Status = resp.StatusCode
	return res
}
