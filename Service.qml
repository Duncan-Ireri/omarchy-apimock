import QtQuick
import Quickshell
import Quickshell.Io
import "Model.js" as Model

// One mock engine per shell session.
//
// The bar builds its widgets once per monitor. A helper owned by the widget
// would be started once per screen — two processes fighting for the same TCP
// port. The shell loads a `service` kind exactly once, so the helper, the live
// state and the IPC target live here; every BarWidget.qml is a view onto this
// object.
Item {
  id: root

  // Injected by the shell.
  property var shell: null
  property var manifest: null

  readonly property string manifestPluginId: "ireri.apimock"
  readonly property int expectedProtocol: 1

  readonly property string pluginDir: manifest && manifest.__sourceDir
    ? String(manifest.__sourceDir)
    : (Quickshell.env("HOME") || "") + "/.config/omarchy/plugins/" + manifestPluginId

  readonly property string home: Quickshell.env("HOME") || ""

  // Configuration comes from shell.json, not from the widgets — the bar injects
  // a widget's `settings` a tick after creation, so a widget's first report is
  // the default, not the persisted value.
  readonly property var configEntry: shell && shell.shellConfig
    ? Model.barEntry(shell.shellConfig, manifestPluginId)
    : null
  readonly property var settings: Model.settingsIn(configEntry ? configEntry.settings : ({}))

  readonly property string mappings: settings.mappings
  readonly property string host: settings.host
  readonly property int port: settings.port
  readonly property bool autoStart: settings.autoStart
  readonly property int journalSize: settings.journalSize

  // ------------------------------------------------------------------- state

  property bool backendReady: false
  property bool protocolOk: true
  property string pluginVersion: ""

  property bool running: false
  property int mappingCount: 0
  property int requestCount: 0
  property int unmatchedCount: 0
  property string lastError: ""
  property string loadedAt: ""
  property var stubs: []

  // Newest-first ring of served requests, for the panel's live log.
  property var journal: []

  property int nextRequestId: 1
  property var pending: ({})
  property bool shuttingDown: false

  // A once-a-second clock the log binds to for "4s ago" without every row
  // running its own timer.
  property int nowSeconds: Math.floor(Date.now() / 1000)
  Timer {
    interval: 1000
    running: root.anyViewOpen
    repeat: true
    triggeredOnStart: true
    onTriggered: root.nowSeconds = Math.floor(Date.now() / 1000)
  }

  property int openViewCount: 0
  readonly property bool anyViewOpen: openViewCount > 0
  function viewOpened() { openViewCount++ }
  function viewClosed() { if (openViewCount > 0) openViewCount-- }

  // ------------------------------------------------------------------ theme

  property var palette: ({})
  FileView {
    path: (Quickshell.env("HOME") || "") + "/.local/state/omarchy/current/theme/colors.toml"
    watchChanges: true
    printErrors: false
    onLoaded: root.palette = Model.parsePalette(text())
    onFileChanged: reload()
    onLoadFailed: root.palette = ({})
  }

  // ----------------------------------------------------------------- helper

  FileView {
    path: root.pluginDir + "/manifest.json"
    printErrors: false
    onLoaded: {
      var parsed = Model.parseLine(text())
      root.pluginVersion = parsed && String(parsed.id || "") === root.manifestPluginId
        ? String(parsed.version || "")
        : ""
    }
  }

  Process {
    id: backend
    property bool launched: false
    command: [root.pluginDir + "/bin/omock", "control"]
    running: false
    stdinEnabled: true
    stdout: SplitParser { onRead: function(line) { root.handleEvent(Model.parseLine(line)) } }
    stderr: SplitParser { onRead: function(line) { if (line) console.warn("omock:", line) } }

    onStarted: backend.launched = true

    // Quickshell reports a command it could not launch by flipping `running`
    // back to false without ever emitting `started`.
    onRunningChanged: {
      if (running) { backend.launched = false; return }
      root.backendReady = false
      root.running = false
      if (!backend.launched) {
        root.lastError = "The omock helper is missing — run ./build.sh in " + root.pluginDir
        return
      }
      if (!root.shuttingDown) restartTimer.restart()
    }

    onExited: function(code) {
      root.backendReady = false
      root.running = false
      if (code !== 0 && !root.shuttingDown) {
        root.lastError = "The omock helper stopped unexpectedly (exit " + code + ")"
      }
    }
  }

  Timer {
    id: restartTimer
    interval: 5000
    repeat: false
    onTriggered: if (!root.shuttingDown && !backend.running) backend.running = true
  }

  Component.onCompleted: backend.running = true
  Component.onDestruction: {
    root.shuttingDown = true
    if (backend.running) root.send({ cmd: "shutdown" })
  }

  // --------------------------------------------------------------------- IPC

  function send(command, onReply) {
    if (!backend.running) return -1
    var id = nextRequestId++
    var payload = { id: id }
    for (var key in command) payload[key] = command[key]
    if (onReply) {
      var copy = pending
      copy[id] = onReply
      pending = copy
    }
    backend.write(JSON.stringify(payload) + "\n")
    return id
  }

  function handleEvent(event) {
    if (!event) return
    switch (String(event.ev || "")) {
    case "ready":
      if (!Model.protocolAccepted(event, expectedProtocol)) {
        protocolOk = false
        lastError = "The omock helper speaks protocol " + event.protocol
          + ", this plugin expects " + expectedProtocol + " — restart the shell"
        backend.running = false
        return
      }
      protocolOk = true
      backendReady = true
      lastError = ""
      send({ cmd: "hello", version: pluginVersion })
      pushConfiguration()
      break

    case "state":
      running = event.running === true
      mappingCount = Number(event.mappingCount) || 0
      requestCount = Number(event.requestCount) || 0
      unmatchedCount = Number(event.unmatchedCount) || 0
      lastError = String(event.lastError || "")
      loadedAt = String(event.loadedAt || "")
      stubs = Model.asList(event.stubs)
      break

    case "request":
      // The helper only pushes a full `state` on config/lifecycle changes, so
      // keep the live counters moving here; a later `state` re-syncs them.
      requestCount++
      if (String(event.matched || "") === "") unmatchedCount++
      var next = Model.asList(journal).slice()
      next.unshift({
        seq: Number(event.seq) || 0,
        ts: String(event.ts || ""),
        method: String(event.method || ""),
        url: String(event.url || ""),
        status: Number(event.status) || 0,
        matched: String(event.matched || ""),
        durationMs: Number(event.durationMs) || 0
      })
      if (next.length > journalSize) next = next.slice(0, journalSize)
      journal = next
      break

    case "log":
      if (String(event.level) === "warn") console.warn("omock:", event.msg)
      break

    case "reply":
      var handler = pending[event.id]
      if (handler) {
        var p = pending
        delete p[event.id]
        pending = p
        handler(event)
      }
      break
    }
  }

  function pushConfiguration() {
    if (!backendReady) return
    send({
      cmd: "configure",
      source: Model.expandTilde(mappings, home),
      host: host,
      port: port,
      autoStart: autoStart,
      journalSize: journalSize
    })
  }

  onMappingsChanged: pushConfiguration()
  onHostChanged: pushConfiguration()
  onPortChanged: pushConfiguration()
  onAutoStartChanged: pushConfiguration()

  // ----------------------------------------------------------------- actions

  function start() { send({ cmd: "start" }) }
  function stop() { send({ cmd: "stop" }) }
  function reload() { send({ cmd: "reload" }) }
  function toggleRunning() { running ? stop() : start() }

  function clearJournal() {
    journal = []
    send({ cmd: "clearJournal" })
  }

  // One-shot validation, independent of the running server, for the panel's
  // "Validate" button.
  property string validateResult: ""
  Process {
    id: validator
    running: false
    property string buf: ""
    stdout: SplitParser { onRead: function(line) { validator.buf += line + "\n" } }
    stderr: SplitParser { onRead: function(line) { validator.buf += line + "\n" } }
    onExited: function(code) {
      root.validateResult = (code === 0 ? "✓ " : "✗ ") + validator.buf.trim()
    }
  }
  function validate() {
    if (validator.running) return
    validator.buf = ""
    validateResult = "Checking…"
    validator.command = [root.pluginDir + "/bin/omock", "validate", "-f", Model.expandTilde(mappings, home)]
    validator.running = true
  }

  // -------------------------------------------------------------- persistence

  function persist(nextMappings, nextHost, nextPort) {
    if (!shell || typeof shell.mutateShellConfig !== "function") {
      lastError = "This Omarchy build cannot save plugin settings"
      return false
    }
    var payload = Model.persistPayload(nextMappings, nextHost, nextPort)
    var id = manifestPluginId
    shell.mutateShellConfig(function(config) {
      var groups = []
      if (config.bar && config.bar.layout) {
        var regions = ["left", "center", "right"]
        for (var r = 0; r < regions.length; r++) {
          if (Array.isArray(config.bar.layout[regions[r]])) groups.push(config.bar.layout[regions[r]])
        }
      }
      if (Array.isArray(config.plugins)) groups.push(config.plugins)
      for (var g = 0; g < groups.length; g++) {
        for (var e = 0; e < groups[g].length; e++) {
          var entry = groups[g][e]
          if (!entry || typeof entry !== "object" || String(entry.id || "") !== id) continue
          for (var field in payload) entry[field] = payload[field]
          return
        }
      }
    })
    return true
  }

  // ---------------------------------------------------------------- IPC target

  function summonView(action) {
    if (!shell || typeof shell[action] !== "function") return "unknown"
    return shell[action](manifestPluginId, "{}") === false ? "unknown" : "ok"
  }

  IpcHandler {
    target: "ireri.apimock"
    function open(): string { return root.summonView("summon") }
    function close(): string { return root.summonView("hide") }
    function toggle(): string { return root.summonView("toggle") }
    function start(): string { root.start(); return "ok" }
    function stop(): string { root.stop(); return "ok" }
    function reload(): string { root.reload(); return "ok" }
    function status(): string {
      return JSON.stringify({
        running: root.running,
        host: root.host,
        port: root.port,
        mappings: root.mappings,
        mappingCount: root.mappingCount,
        requestCount: root.requestCount,
        unmatchedCount: root.unmatchedCount,
        lastError: root.lastError
      })
    }
  }
}
