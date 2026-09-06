import QtQuick
import Quickshell
import qs.Ui
import qs.Commons
import "Model.js" as Model

// The bar button and its popup, built once per monitor. It owns no helper and
// no state that matters — all of that lives in Service.qml, loaded once by the
// shell. This is a view onto that engine plus this popup's own open state.
Panel {
  id: root
  moduleName: "ireri.apimock"

  // The IPC target belongs to the service (there is one); a per-monitor widget
  // registering it would collide.
  manageIpc: false

  readonly property string manifestPluginId: "ireri.apimock"
  readonly property var engine: bar && bar.shell && typeof bar.shell.serviceFor === "function"
    ? bar.shell.serviceFor(manifestPluginId)
    : null

  readonly property color foreground: bar ? bar.foreground : Color.foreground
  readonly property color urgent: bar ? bar.urgent : Color.urgent
  readonly property color accent: Color.accent
  readonly property color dim: Qt.darker(foreground, 1.5)
  readonly property string fontFamily: bar ? bar.fontFamily : Style.font.family
  readonly property int smallFont: Math.max(8, Math.round(Style.font.body * 0.85))

  // ------------------------------------------------------------ engine state

  readonly property bool backendReady: engine ? engine.backendReady : false
  readonly property bool running: engine ? engine.running : false
  readonly property string lastError: engine ? engine.lastError : "The API Mock engine is not loaded."
  readonly property int mappingCount: engine ? engine.mappingCount : 0
  readonly property int requestCount: engine ? engine.requestCount : 0
  readonly property int unmatchedCount: engine ? engine.unmatchedCount : 0
  readonly property int port: engine ? engine.port : 0
  readonly property string mappings: engine ? engine.mappings : ""
  readonly property var stubs: engine ? engine.stubs : []
  readonly property var journal: engine ? engine.journal : []
  readonly property var palette: engine ? engine.palette : ({})
  readonly property int nowSeconds: engine ? engine.nowSeconds : Math.floor(Date.now() / 1000)
  readonly property string validateResult: engine ? engine.validateResult : ""

  // ------------------------------------------------------------- per-view state

  property string draftMappings: ""
  property int draftPort: 8080
  property bool viewRegistered: false

  implicitWidth: button.implicitWidth
  implicitHeight: button.implicitHeight

  function syncOpenState() {
    if (!engine || opened === viewRegistered) return
    viewRegistered = opened
    if (opened) engine.viewOpened()
    else engine.viewClosed()
  }
  onEngineChanged: { viewRegistered = false; syncOpenState() }
  Component.onDestruction: if (engine && viewRegistered) engine.viewClosed()

  onOpenedChanged: {
    if (opened) {
      draftMappings = mappings
      draftPort = port > 0 ? port : 8080
    }
    syncOpenState()
  }

  function commitSettings() {
    if (!engine) return
    if (draftMappings === mappings && draftPort === port) return
    engine.persist(draftMappings, engine.host, draftPort)
  }

  // ------------------------------------------------------------- status colour

  function themed(kind, fallback) {
    var c = Model.statusColor(palette, kind)
    return c !== "" ? c : fallback
  }
  readonly property color okColor: themed("ok", accent)
  readonly property color warnColor: themed("warn", foreground)

  readonly property color badgeColor: {
    if (!backendReady || lastError !== "") return urgent
    if (running) return okColor
    return dim
  }
  readonly property bool badgeVisible: true

  readonly property string statusLine: {
    if (!backendReady) return "engine not loaded"
    if (lastError !== "") return lastError
    if (!running) return "stopped"
    var s = ":" + port + " · " + Model.plural(mappingCount, "stub")
    if (requestCount > 0) {
      s += " · " + Model.plural(requestCount, "req")
      if (unmatchedCount > 0) s += " · " + unmatchedCount + " unmatched"
    }
    return s
  }

  // ------------------------------------------------------------------- bar

  BarIconButton {
    id: button
    anchors.fill: parent
    bar: root.bar
    text: "" // server
    active: root.running
    tooltipText: Model.tooltipFor({
      running: root.running, port: root.port, mappings: root.mappings,
      mappingCount: root.mappingCount, requestCount: root.requestCount,
      unmatchedCount: root.unmatchedCount, lastError: root.backendReady ? root.lastError : ""
    })
    onPressed: function(b) { root.toggle() }

    iconComponent: Component {
      Item {
        anchors.fill: parent
        Item {
          id: mark
          anchors.centerIn: parent
          width: Math.round(Style.bar.iconFont * 0.95)
          height: width
          Text {
            anchors.centerIn: parent
            text: ""
            color: root.running ? root.foreground : root.dim
            font.family: root.fontFamily
            font.pixelSize: Style.bar.iconFont
          }
          Rectangle {
            visible: root.badgeVisible
            width: Math.max(4, Math.round(parent.width * 0.4))
            height: width
            radius: width / 2
            anchors.right: parent.right
            anchors.bottom: parent.bottom
            anchors.rightMargin: -2
            anchors.bottomMargin: -2
            color: root.badgeColor
            border.width: 1
            border.color: Color.popups.background
          }
        }
      }
    }
  }

  // ----------------------------------------------------------------- popup

  KeyboardPanel {
    id: panel
    anchorItem: button
    owner: root
    bar: root.bar
    open: root.opened
    focusTarget: keyCatcher
    contentWidth: panel.fittedContentWidth(Style.space(420))
    contentHeight: panel.fittedContentHeight(column.implicitHeight, Style.space(600))

    PanelKeyCatcher {
      id: keyCatcher
      anchors.fill: parent
      blocked: mappingsField.activeFocus || portField.field.activeFocus
      onCloseRequested: root.close()
      onTabRequested: function(direction) { root.switchPanel(direction) }
      onTextKey: function(text) {
        if (mappingsField.activeFocus || portField.field.activeFocus) return
        if (text === "s" && root.engine) root.engine.toggleRunning()
        else if (text === "r" && root.engine) root.engine.reload()
        else if (text === "v" && root.engine) root.engine.validate()
      }

      Column {
        id: column
        width: parent.width
        spacing: Style.space(12)

        PanelHero {
          width: parent.width
          title: "API Mock"
          meta: root.statusLine
          foreground: root.lastError !== "" && root.backendReady ? root.urgent : root.foreground
          fontFamily: root.fontFamily
          iconComponent: Component {
            Text {
              text: ""
              color: root.running ? root.okColor : root.dim
              font.family: root.fontFamily
              font.pixelSize: Style.font.display
            }
          }
        }

        // ----------------------------------------------------------- actions

        Row {
          width: parent.width
          spacing: Style.space(8)

          Button {
            text: root.running ? "Stop" : "Start"
            bordered: true
            focusable: true
            enabled: root.backendReady
            foreground: root.running ? root.urgent : root.okColor
            onClicked: if (root.engine) root.engine.toggleRunning()
          }
          Button {
            text: "Reload"
            bordered: true
            focusable: true
            enabled: root.backendReady && root.running
            onClicked: if (root.engine) root.engine.reload()
          }
          Button {
            text: "Validate"
            bordered: true
            focusable: true
            enabled: root.backendReady
            onClicked: if (root.engine) root.engine.validate()
          }
        }

        Text {
          width: parent.width
          visible: text !== ""
          text: root.validateResult
          wrapMode: Text.Wrap
          color: root.validateResult.slice(0, 1) === "✓" ? root.okColor
               : root.validateResult.slice(0, 1) === "✗" ? root.urgent : root.dim
          font.family: root.fontFamily
          font.pixelSize: root.smallFont
        }

        // ---------------------------------------------------------- settings

        Column {
          width: parent.width
          spacing: Style.space(6)

          PanelSectionHeader { text: "MAPPINGS FILE OR FOLDER"; foreground: root.foreground }

          TextField {
            id: mappingsField
            width: parent.width
            foreground: root.foreground
            placeholderText: "~/mocks/mappings.json"
            text: root.draftMappings
            onTextChanged: root.draftMappings = text
            onEditingFinished: root.commitSettings()
          }
        }

        Row {
          width: parent.width
          spacing: Style.space(16)

          NumberField {
            id: portField
            label: "PORT"
            foreground: root.foreground
            from: 1
            to: 65535
            value: root.draftPort
            onModified: function(v) { root.draftPort = v; root.commitSettings() }
            // A port is not a quantity — drop the locale's thousands separator.
            Component.onCompleted: field.textFromValue = function(v, locale) { return "" + v }
          }

          Column {
            spacing: Style.space(2)
            Text {
              text: "BIND"
              color: Qt.darker(root.foreground, 1.4)
              font.family: root.fontFamily
              font.pixelSize: Style.font.bodySmall
              font.bold: true
            }
            Text {
              text: root.engine ? root.engine.host : "127.0.0.1"
              color: root.foreground
              font.family: root.fontFamily
              font.pixelSize: Style.font.body
            }
          }
        }

        // ------------------------------------------------------------- error

        Rectangle {
          width: parent.width
          visible: root.backendReady && root.lastError !== ""
          implicitHeight: errText.implicitHeight + Style.space(12)
          radius: Style.cornerRadius
          color: Qt.rgba(root.urgent.r, root.urgent.g, root.urgent.b, 0.12)
          Text {
            id: errText
            anchors.fill: parent
            anchors.margins: Style.space(6)
            text: root.lastError
            wrapMode: Text.Wrap
            color: root.urgent
            font.family: root.fontFamily
            font.pixelSize: root.smallFont
          }
        }

        // ------------------------------------------------------------- stubs

        Column {
          width: parent.width
          spacing: Style.space(4)
          visible: root.stubs && root.stubs.length > 0

          PanelSectionHeader { text: "STUBS (" + (root.stubs ? root.stubs.length : 0) + ")"; foreground: root.foreground }

          Repeater {
            model: root.stubs
            delegate: Row {
              required property var modelData
              width: column.width
              spacing: Style.space(8)
              Text {
                text: String(modelData.method || "ANY")
                color: root.dim
                font.family: root.fontFamily
                font.pixelSize: root.smallFont
                width: Style.space(46)
              }
              Text {
                width: parent.width - Style.space(46) - parent.spacing
                elide: Text.ElideRight
                text: String(modelData.url || "*")
                color: root.foreground
                font.family: root.fontFamily
                font.pixelSize: root.smallFont
              }
            }
          }
        }

        // ---------------------------------------------------------- requests

        Column {
          width: parent.width
          spacing: Style.space(4)

          Row {
            width: parent.width
            PanelSectionHeader {
              text: "REQUESTS"
              foreground: root.foreground
              width: parent.width - clearBtn.width
            }
            Button {
              id: clearBtn
              text: "Clear"
              fontSize: root.smallFont
              visible: root.journal && root.journal.length > 0
              onClicked: if (root.engine) root.engine.clearJournal()
            }
          }

          Text {
            visible: !root.journal || root.journal.length === 0
            text: root.running ? "Waiting for requests…" : "Start the server to see requests."
            color: root.dim
            font.family: root.fontFamily
            font.pixelSize: root.smallFont
          }

          Repeater {
            model: root.journal
            delegate: Row {
              required property var modelData
              width: column.width
              spacing: Style.space(8)

              readonly property string cls: Model.statusClass(modelData.status)

              Text {
                text: String(modelData.method || "")
                color: root.dim
                font.family: root.fontFamily
                font.pixelSize: root.smallFont
                width: Style.space(46)
              }
              Text {
                width: parent.width - Style.space(46) - Style.space(94) - parent.spacing * 2
                elide: Text.ElideRight
                text: String(modelData.url || "")
                color: root.foreground
                font.family: root.fontFamily
                font.pixelSize: root.smallFont
              }
              Text {
                width: Style.space(52)
                horizontalAlignment: Text.AlignRight
                text: String(modelData.status || "")
                    + (modelData.matched === "" ? " ∅" : "")
                color: parent.cls === "ok" ? root.okColor
                     : parent.cls === "warn" ? root.warnColor
                     : parent.cls === "error" ? root.urgent : root.dim
                font.family: root.fontFamily
                font.pixelSize: root.smallFont
              }
              Text {
                width: Style.space(38)
                horizontalAlignment: Text.AlignRight
                text: Model.fmtAgo(root.nowSeconds - Math.floor(Date.parse(modelData.ts) / 1000))
                color: root.dim
                font.family: root.fontFamily
                font.pixelSize: root.smallFont
              }
            }
          }
        }
      }
    }
  }
}
