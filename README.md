# API Mock

A local, WireMock-style HTTP mock server for building and testing APIs and
webhooks — driven from the Omarchy bar, or run standalone from the terminal.

Point it at a JSON file of request → response stubs, choose a port, hit it from
the app you're building. Requests show up in a live log as they arrive, matched
or unmatched.

- **Bar widget + panel** — start/stop the server, pick the mappings file, set
  the port, watch requests, reload, validate.
- **`omock` CLI** — the same engine without Omarchy, for scripts and CI.
- **WireMock-compatible mappings** — most existing WireMock stub files load
  as-is.
- **Zero dependencies** — one static Go binary, stdlib only.

## Layout

```
manifest.json  Service.qml  BarWidget.qml  Model.js   the Omarchy plugin
install.sh                                             fetch the prebuilt omock helper
build.sh       dev-sync.sh                             build from source / local install
backend/                                               the Go mock engine (omock)
examples/petstore.json                                 a sample mappings file
```

## Install

```bash
omarchy plugin add https://github.com/Duncan-Ireri/omarchy-apimock
cd ~/.config/omarchy/plugins/ireri.apimock
./install.sh                 # downloads the checksum-verified omock helper (Linux x86_64)
#   …or, on another arch / to build from source (needs Go):
#   ./build.sh
omarchy plugin enable ireri.apimock
omarchy restart shell
```

Then set the mappings file and port in the widget's panel (or in Setup →
Plugins → API Mock settings), and press **Start**.

### Uninstall

```bash
omarchy plugin remove ireri.apimock
```

That removes the plugin directory and its bar entry. The plugin writes nothing
outside `~/.config/omarchy/shell.json` (its own settings) and never touches
system files.

### Local development

```bash
./dev-sync.sh                 # build + copy into the plugins dir + rescan
```

Re-run after editing QML or Go.

## Standalone use

```bash
./build.sh
./bin/omock serve -f examples/petstore.json -p 8080 -v
curl localhost:8080/pets/1

./bin/omock validate -f examples/petstore.json
```

The server hot-reloads whenever the mappings file (or any `*.json` in the
mappings folder) changes on disk.

## Mappings format

A file is `{ "mappings": [ <stub>, ... ] }`, a bare `[ <stub>, ... ]`, or a
single `<stub>`. A directory loads every `*.json` inside it.

```jsonc
{
  "name": "get pet by id",
  "priority": 5,                       // lower wins; default 5
  "request": {
    "method": "GET",                   // verb, or "ANY"
    "urlPathPattern": "/pets/[0-9]+",  // or urlPath / url / urlPattern
    "queryParameters": { "detail": { "equalTo": "full" } },
    "headers": { "Accept": { "contains": "json" } },
    "bodyPatterns": [
      { "matchesJsonPath": { "expression": "$.name", "equalTo": "Rex" } }
    ]
  },
  "response": {
    "status": 200,
    "headers": { "Content-Type": "application/json" },
    "jsonBody": { "id": 1, "name": "Rex" },   // or body / base64Body / bodyFileName
    "fixedDelayMilliseconds": 0
  }
}
```

### Matchers

`equalTo` (+ `caseInsensitive`), `contains`, `matches` (regex),
`doesNotMatch`, `absent`, `equalToJson` (+ `ignoreArrayOrder`,
`ignoreExtraElements`), `matchesJsonPath` (string, or
`{ "expression", "equalTo" }`). The JSONPath subset supports `$`, `.key`,
`['key']` and `[n]`.

### URL matching (first one present wins)

`urlPath` (exact path) · `urlPathPattern` (regex on path) · `url` (exact path +
query) · `urlPattern` (regex on path + query).

### Unmatched requests

Return `404` with `X-Apimock-Unmatched: true` and a JSON body naming the
closest stubs and why each missed.

### Webhooks (`postServeActions`)

After the response is sent, fire an outbound HTTP call — for testing a webhook
receiver in the app you're building:

```json
"postServeActions": [
  {
    "name": "webhook",
    "parameters": {
      "method": "POST",
      "url": "http://127.0.0.1:4000/webhooks/orders",
      "headers": { "Content-Type": "application/json" },
      "jsonBody": { "event": "order.created", "id": 123 },
      "delayMilliseconds": 250
    }
  }
]
```

## Not yet supported

Response templating (`{{request.*}}`), proxy/record mode, the verification
admin API, stateful scenarios, more than one server at a time.

## Security

The `omock` helper runs unsandboxed inside `omarchy-shell` with your user
permissions, like every Omarchy plugin. It only listens on the address you
configure (`127.0.0.1` by default), reads the mappings file you point it at,
and — if a stub declares one — makes the outbound webhook call that stub
specifies.

## License

MIT — see [LICENSE](LICENSE).
