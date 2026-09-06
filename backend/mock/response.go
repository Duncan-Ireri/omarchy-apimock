package mock

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// render writes the stub's response to w. baseDir resolves bodyFileName.
func (resp *Response) render(w http.ResponseWriter, baseDir string) (int, error) {
	if resp.FixedDelayMs > 0 {
		d := time.Duration(resp.FixedDelayMs) * time.Millisecond
		// The server sets a WriteTimeout to fend off slow clients; a stub that
		// asks for a delay is a deliberate wait, so push this connection's write
		// deadline out past it. SetWriteDeadline is best-effort (unsupported on
		// the recorder used in some tests) — ignore the error.
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(d + 30*time.Second))
		time.Sleep(d)
	}

	body, ctype, err := resp.bodyBytes(baseDir)
	if err != nil {
		http.Error(w, "mock: "+err.Error(), http.StatusInternalServerError)
		return http.StatusInternalServerError, err
	}

	header := w.Header()
	for name, vals := range normalizeHeaders(resp.Headers) {
		for _, v := range vals {
			header.Add(name, v)
		}
	}
	if ctype != "" && header.Get("Content-Type") == "" {
		header.Set("Content-Type", ctype)
	}

	status := resp.status()
	w.WriteHeader(status)
	if len(body) > 0 {
		_, _ = w.Write(body)
	}
	return status, nil
}

func (resp *Response) bodyBytes(baseDir string) (body []byte, contentType string, err error) {
	switch {
	case len(resp.JsonBody) > 0:
		// Re-marshal so formatting is normalised and invalid JSON is caught.
		var v any
		if err := json.Unmarshal(resp.JsonBody, &v); err != nil {
			return nil, "", err
		}
		b, err := json.Marshal(v)
		if err != nil {
			return nil, "", err
		}
		return b, "application/json", nil
	case resp.Body != nil:
		return []byte(*resp.Body), "", nil
	case resp.Base64Body != nil:
		b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(*resp.Base64Body))
		if err != nil {
			return nil, "", err
		}
		return b, "", nil
	case resp.BodyFileName != "":
		clean := filepath.Clean(resp.BodyFileName)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			return nil, "", os.ErrPermission
		}
		b, err := os.ReadFile(filepath.Join(baseDir, clean))
		if err != nil {
			return nil, "", err
		}
		return b, contentTypeByExt(clean), nil
	default:
		return nil, "", nil
	}
}

func contentTypeByExt(name string) string {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".json":
		return "application/json"
	case ".xml":
		return "application/xml"
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".txt":
		return "text/plain; charset=utf-8"
	case ".csv":
		return "text/csv"
	default:
		return ""
	}
}
