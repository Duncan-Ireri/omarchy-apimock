# Changelog

## 0.1.0

Initial release.

- WireMock-style local mock server (`omock`), zero external dependencies.
  - `omock serve -f <file|dir> -p <port>` — standalone server with mtime hot-reload.
  - `omock validate -f <file|dir>` — parse-check mappings, non-zero exit on failure.
  - `omock control` — JSON-lines protocol used by the Omarchy plugin.
- Matching: `method`, `urlPath` / `urlPathPattern` / `url` / `urlPattern`,
  `queryParameters` / `headers` / `cookies` matchers
  (`equalTo` + `caseInsensitive`, `contains`, `matches`, `doesNotMatch`,
  `absent`), `bodyPatterns` including `equalToJson`
  (`ignoreArrayOrder` / `ignoreExtraElements`) and a `matchesJsonPath` subset,
  `priority`.
- Responses: `status`, `headers`, `jsonBody` / `body` / `base64Body` /
  `bodyFileName`, `fixedDelayMilliseconds`.
- `postServeActions` outbound webhooks.
- Unmatched requests return `404` with `X-Apimock-Unmatched` and a
  closest-miss diagnostic body.
- Omarchy bar widget + panel: start/stop, mappings path, port, stub list,
  live request log, reload, validate. Settings persist to `shell.json`.
