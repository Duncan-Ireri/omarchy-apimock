// Pure helpers for ireri.apimock. No QML types, no engine state — everything is
// a plain function of its arguments so the service and the widget can both use
// it, and so it is testable on its own.
//
// QML loads this with `import "Model.js" as Model`.

// Coerce anything list-shaped (including QML sequence wrappers, on which
// Array.isArray lies) into a real array.
function asList(value) {
  if (!value) return []
  if (Array.isArray(value)) return value
  if (typeof value !== "object") return []
  var n = Number(value.length)
  if (!isFinite(n) || n <= 0) return []
  var out = []
  for (var i = 0; i < n; i++) out.push(value[i])
  return out
}

// Find this plugin's entry in shell.json (bar layout or plugins[]) and split it
// into { id, settings }. The entry is the source of truth, not the `settings`
// the bar injects a tick after widget creation.
function barEntry(config, pluginId) {
  var id = String(pluginId || "")
  if (!config || typeof config !== "object" || id === "") return null
  var groups = []
  var layout = config.bar && typeof config.bar === "object" ? config.bar.layout : null
  var regions = ["left", "center", "right"]
  for (var r = 0; r < regions.length; r++) {
    if (layout) groups.push(asList(layout[regions[r]]))
  }
  groups.push(asList(config.plugins))
  for (var g = 0; g < groups.length; g++) {
    for (var e = 0; e < groups[g].length; e++) {
      var entry = groups[g][e]
      if (!entry || typeof entry !== "object") continue
      if (String(entry.id || "") !== id) continue
      var settings = {}
      for (var key in entry) if (key !== "id") settings[key] = entry[key]
      return { id: String(entry.id), settings: settings }
    }
  }
  return null
}

// Effective, clamped settings — bounds match what the helper accepts, so the
// panel can never show a value the helper would reject.
function settingsIn(settings) {
  var raw = settings && typeof settings === "object" ? settings : {}
  return {
    mappings: String(raw.mappings || "~/mocks/mappings.json").trim(),
    host: String(raw.host || "127.0.0.1").trim() || "127.0.0.1",
    port: clamp(numberOr(raw.port, 8080), 1, 65535),
    autoStart: raw.autoStart === true,
    journalSize: clamp(numberOr(raw.journalSize, 200), 10, 5000)
  }
}

function numberOr(value, fallback) {
  var n = Number(value)
  return isFinite(n) ? n : fallback
}

function clamp(value, low, high) {
  return Math.max(low, Math.min(high, Math.round(value)))
}

// Parse one line of JSON from the helper. Returns null on anything malformed —
// a partial write or a stray log line is expected, not exceptional.
function parseLine(line) {
  var text = String(line || "").trim()
  if (text === "" || (text[0] !== "{" && text[0] !== "[")) return null
  try {
    return JSON.parse(text)
  } catch (e) {
    return null
  }
}

function protocolAccepted(event, expected) {
  var got = Number(event && event.protocol)
  return !isFinite(got) || got === expected
}

// Read an Omarchy theme colors.toml into { name: "#rrggbb" }. The file is a flat
// list of `key = "#value"` lines plus section headers we ignore.
function parsePalette(text) {
  var out = {}
  var lines = String(text || "").split("\n")
  for (var i = 0; i < lines.length; i++) {
    var line = lines[i].trim()
    if (line === "" || line[0] === "#" || line[0] === "[") continue
    var eq = line.indexOf("=")
    if (eq < 0) continue
    var key = line.slice(0, eq).trim()
    var val = line.slice(eq + 1).trim().replace(/^["']|["']$/g, "")
    if (/^#?[0-9a-fA-F]{6,8}$/.test(val)) {
      out[key] = val[0] === "#" ? val : "#" + val
    }
  }
  return out
}

// Map a status kind to a theme colour, falling back to "" so the caller can use
// its own default.
function statusColor(palette, kind) {
  var p = palette && typeof palette === "object" ? palette : {}
  if (kind === "ok") return p.color2 || p.green || ""
  if (kind === "error") return p.color1 || p.red || ""
  if (kind === "warn") return p.color3 || p.yellow || ""
  return ""
}

function expandTilde(path, home) {
  var p = String(path || "")
  if (p === "~") return String(home || "")
  if (p.slice(0, 2) === "~/") return String(home || "") + p.slice(1)
  return p
}

// "2s" / "4m" / "1h" — coarse, because the log is a stream not a stopwatch.
function fmtAgo(seconds) {
  var s = Math.max(0, Math.floor(Number(seconds) || 0))
  if (s < 60) return s + "s"
  if (s < 3600) return Math.floor(s / 60) + "m"
  if (s < 86400) return Math.floor(s / 3600) + "h"
  return Math.floor(s / 86400) + "d"
}

function tooltipFor(state) {
  if (!state) return "API Mock"
  if (state.lastError) return "API Mock — " + state.lastError
  if (!state.running) {
    return "API Mock — stopped" + (state.mappings ? " (" + baseName(state.mappings) + ")" : "")
  }
  var bits = ["mock", ":" + state.port, plural(state.mappingCount, "stub")]
  var reqs = Number(state.requestCount) || 0
  if (reqs > 0) {
    var line = plural(reqs, "req")
    var un = Number(state.unmatchedCount) || 0
    if (un > 0) line += " (" + un + " unmatched)"
    bits.push(line)
  }
  return bits.join(" · ")
}

function plural(n, word) {
  var count = Number(n) || 0
  return count + " " + word + (count === 1 ? "" : "s")
}

function baseName(path) {
  var p = String(path || "").replace(/\/+$/, "")
  var slash = p.lastIndexOf("/")
  return slash < 0 ? p : p.slice(slash + 1)
}

// The subset of the settings the plugin panel owns and writes back to
// shell.json. Kept narrow so Omarchy's own bar-widget settings editor, which
// writes the same entry from the manifest schema, is never clobbered.
function persistPayload(mappings, host, port) {
  return {
    mappings: String(mappings || "").trim(),
    host: String(host || "127.0.0.1").trim() || "127.0.0.1",
    port: clamp(numberOr(port, 8080), 1, 65535)
  }
}

function normalizeJournal(entries, cap) {
  var list = asList(entries)
  var out = []
  for (var i = 0; i < list.length; i++) {
    var e = list[i]
    if (!e || typeof e !== "object") continue
    out.push({
      seq: Number(e.seq) || 0,
      ts: String(e.ts || ""),
      method: String(e.method || ""),
      url: String(e.url || ""),
      status: Number(e.status) || 0,
      matched: String(e.matched || ""),
      durationMs: Number(e.durationMs) || 0
    })
  }
  out.sort(function (a, b) { return b.seq - a.seq })
  if (cap > 0 && out.length > cap) out = out.slice(0, cap)
  return out
}

function statusClass(status) {
  var s = Number(status) || 0
  if (s >= 500) return "error"
  if (s === 404 || s >= 400) return "warn"
  if (s >= 200 && s < 300) return "ok"
  return "muted"
}
