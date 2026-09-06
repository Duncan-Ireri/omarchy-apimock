# Changelog

## 0.1.0 — 2026-09-06

First release.

The mock server handles WireMock-style matching (method; the `urlPath`,
`urlPathPattern`, `url` and `urlPattern` forms; query, header, cookie and body
matchers including `equalToJson` and a `matchesJsonPath` subset; `priority`) and
builds responses from JSON, a string, base64 or a file, with an optional delay.
`postServeActions` sends an outbound webhook. It watches the mappings file so
edits take effect without a restart, and answers an unmatched request with a 404
that says which stubs came closest.

Ships as the `omock` binary (`serve`, `validate`, and the `control` protocol the
plugin speaks) and an Omarchy bar widget that starts and stops the server and
shows requests as they arrive. Widget settings are stored in `shell.json`.
