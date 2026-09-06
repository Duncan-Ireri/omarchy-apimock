# API Mock

A local HTTP mock server for working on APIs and webhooks. You give it a JSON
file of stubs in WireMock's format and it serves them on a port you pick. There
is an Omarchy bar widget to start and stop it and watch requests go by, and the
`omock` binary runs on its own for scripts and CI.

Existing WireMock stub files mostly load without changes. The binary is Go with
no third-party dependencies.

## Install

```bash
omarchy plugin add https://github.com/Duncan-Ireri/omarchy-apimock
cd ~/.config/omarchy/plugins/ireri.apimock
./install.sh
omarchy plugin enable ireri.apimock
omarchy restart shell
```

`install.sh` downloads the `omock` binary from the GitHub release and checks it
against the SHA-256 pinned in `checksums/`, which is committed to this repo and
reviewed with the rest of the source — not against a checksum fetched from the
same release. The build is reproducible (`-trimpath -buildvcs=false`,
`CGO_ENABLED=0`, and the Go toolchain pinned in `backend/go.mod`), so you can
verify it yourself:

```bash
./build.sh
sha256sum bin/omock            # compare with checksums/omock-v<version>-linux-x86_64.sha256
```

There is only a Linux x86_64 build; on anything else run `./build.sh`, which
needs the Go toolchain and produces the same bytes the release ships.

Set the mappings path and port in the widget panel (or under Setup → Plugins →
API Mock), then press Start.

To remove it, `omarchy plugin remove ireri.apimock`. That deletes the plugin
directory and its bar entry. The only file the plugin writes is its own section
of `~/.config/omarchy/shell.json`.

## Without Omarchy

```bash
./build.sh
./bin/omock serve -f examples/petstore.json -p 8080 -v
./bin/omock validate -f examples/petstore.json
```

`serve` re-reads the mappings whenever the file, or any `.json` in the folder
you pointed it at, changes on disk.

## Mappings

The file is `{"mappings": [ <stub>, ... ]}`. A bare array or a single stub object
also work, and if you point at a directory it loads every `.json` in it.

```jsonc
{
  "name": "get pet by id",
  "priority": 5,                        // lower wins, default 5
  "request": {
    "method": "GET",                    // or "ANY"
    "urlPathPattern": "/pets/[0-9]+",   // or urlPath, url, urlPattern
    "queryParameters": { "detail": { "equalTo": "full" } },
    "headers": { "Accept": { "contains": "json" } },
    "bodyPatterns": [
      { "matchesJsonPath": { "expression": "$.name", "equalTo": "Rex" } }
    ]
  },
  "response": {
    "status": 200,
    "headers": { "Content-Type": "application/json" },
    "jsonBody": { "id": 1, "name": "Rex" },   // or body, base64Body, bodyFileName
    "fixedDelayMilliseconds": 0
  }
}
```

URL matching uses the first of these that is present: `urlPath` (exact path),
`urlPathPattern` (regex against the path), `url` (exact path and query),
`urlPattern` (regex against both).

Value matchers, used for query parameters, headers, cookies and body patterns:
`equalTo` (with optional `caseInsensitive`), `contains`, `matches`,
`doesNotMatch`, `absent`, `equalToJson` (with `ignoreArrayOrder` and
`ignoreExtraElements`), and `matchesJsonPath`. JSONPath support is a subset:
`$`, `.key`, `['key']`, `[n]`.

A request that matches nothing returns 404 with an `X-Apimock-Unmatched` header
and a body listing the stubs that came closest and where each one diverged.

### Webhooks

`postServeActions` sends an HTTP request after the response goes out, which
helps when the code you are testing expects a callback:

```json
"postServeActions": [
  {
    "name": "webhook",
    "parameters": {
      "method": "POST",
      "url": "http://127.0.0.1:4000/webhooks/orders",
      "jsonBody": { "event": "order.created", "id": 123 },
      "delayMilliseconds": 250
    }
  }
]
```

## What it does not do

No response templating, no proxy or record mode, no admin or verification API,
no stateful scenarios, one server at a time. Reach for WireMock itself if you
need those.

## Notes

Like every Omarchy plugin, `omock` runs inside `omarchy-shell` with your user
account's permissions and is not sandboxed. It listens on the address in its
settings (127.0.0.1 unless you change it), reads the mappings file you gave it,
and only makes a webhook call when a stub asks for one.

Setting the bind address to `0.0.0.0` exposes the mock to your network: anyone
who can reach the port can drive it. The server caps request header and body
sizes, applies read/write/idle timeouts, and bounds how many webhook calls can
be in flight at once (extra ones are dropped and logged), but it does no
authentication. Keep it on loopback unless you specifically need otherwise.

To work on the plugin, `./dev-sync.sh` builds it and copies it into the plugins
directory and triggers a reload. Run it again after each change.

## License

MIT.
