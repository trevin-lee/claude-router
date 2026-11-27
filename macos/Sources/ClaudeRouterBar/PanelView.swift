import AppKit
import RouterKit
import SwiftUI

/// The menu bar panel, laid out like the system's own menu bar menus
/// (Wi-Fi, Bluetooth, Battery): a bold title row, section headers, rows with
/// a round icon that highlight on hover, and menu items at the bottom.
struct PanelView: View {
    @Bindable var model: RouterModel

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            titleRow

            if let error = model.error {
                ErrorRow(message: error, dismiss: model.dismissError)
            }

            MenuSeparator()

            SectionHeader(title: "Accounts")
            ForEach(model.accounts) { account in
                AccountRow(account: account, model: model)
            }
            if model.accounts.isEmpty && model.loadedOnce && model.error == nil {
                Text("No accounts yet. Run claude-router login 0 in a terminal.")
                    .font(.subheadline)
                    .foregroundStyle(.secondary)
                    .padding(.horizontal, 14)
                    .padding(.vertical, 4)
            }

            MenuSeparator()

            MenuItemRow(title: model.isReadingLive ? "Reading Usage…" : "Refresh Usage", shortcut: "⌘R") {
                Task { await model.readLive() }
            }
            .keyboardShortcut("r")
            .disabled(model.isReadingLive)
            MenuItemRow(title: model.isSettingUp ? "Syncing Shared Setup…" : "Sync Shared Setup") {
                Task { await model.runSetup() }
            }
            .disabled(model.isSettingUp)
            .help("Re-link every account to ~/.claude and sync MCP servers and trusted folders")
            MenuItemRow(title: "Show Routing Log", action: model.openLog)

            MenuSeparator()

            ToggleRow(title: "Open at Login", isOn: $model.launchesAtLogin)

            MenuSeparator()

            MenuItemRow(title: "Quit Claude Router", shortcut: "⌘Q") { NSApp.terminate(nil) }
                .keyboardShortcut("q")
        }
        .padding(.vertical, 5)
        .frame(width: 300)
        // The menu bar keeps this view alive between clicks, so onAppear only
        // fires once; the panel's window becoming key marks every opening.
        .onReceive(NotificationCenter.default.publisher(for: NSWindow.didBecomeKeyNotification)) { _ in
            model.panelOpened()
        }
    }

    private var titleRow: some View {
        HStack {
            Text("Claude Router")
                .font(.system(size: 13, weight: .semibold))
            Spacer()
            if model.isReadingLive {
                ProgressView()
                    .controlSize(.mini)
                    .help("Reading every account's usage")
            }
        }
        .frame(height: 22)
        .padding(.horizontal, 14)
        .padding(.vertical, 3)
    }
}

// MARK: Rows

struct AccountRow: View {
    let account: Account
    let model: RouterModel

    var body: some View {
        HighlightRow(action: showMenu) {
            HStack(alignment: .top, spacing: 8) {
                icon
                VStack(alignment: .leading, spacing: 1) {
                    HStack(alignment: .firstTextBaseline) {
                        Text(account.name)
                            .font(.system(size: 13))
                        Spacer(minLength: 6)
                        Text(trailing)
                            .font(.system(size: 11))
                            .foregroundStyle(.secondary)
                            .monospacedDigit()
                    }
                    Text(subtitle)
                        .font(.system(size: 11))
                        .foregroundStyle(.secondary)
                        .lineLimit(1)
                        .truncationMode(.middle)
                    if account.isSignedIn {
                        ForEach(account.limits, id: \.label) { limit in
                            LimitLine(limit: limit)
                                .padding(.top, 4)
                        }
                    }
                }
            }
            .padding(.vertical, 3)
        }
        .help(account.next ? "New sessions start on this account" : "")
    }

    /// A filled blue circle marks the account the next session starts on,
    /// as the Wi-Fi menu marks the network in use.
    private var icon: some View {
        Image(systemName: account.isSignedIn ? "person.fill" : "person.slash.fill")
            .font(.system(size: 12, weight: .medium))
            .foregroundStyle(account.next ? AnyShapeStyle(.white) : AnyShapeStyle(.primary))
            .frame(width: 26, height: 26)
            .background(account.next ? AnyShapeStyle(Color.accentColor) : AnyShapeStyle(.quaternary),
                        in: .circle)
    }

    private var subtitle: String {
        var parts: [String] = []
        if account.next { parts.append("Next session") }
        if let email = account.email { parts.append(email) }
        if let plan = account.planLabel { parts.append(plan) }
        if parts.isEmpty && !account.isSignedIn { return "Click to sign in" }
        return parts.joined(separator: " · ")
    }

    private var trailing: String {
        switch account.state {
        case .ok:
            if let room = account.room { return room == 0 ? "Full" : "\(room)% left" }
            if let age = account.ageSeconds { return "Read \(ageText(seconds: age))" }
            return ""
        case .signedOut: return "Signed Out"
        case .missing: return "Not Set Up"
        case .unread:
            return FileManager.default.fileExists(atPath: account.folder) ? "Not Read" : "Not Set Up"
        case .unknown: return ""
        }
    }

    private func showMenu() {
        var items: [NativeMenu.Item] = []
        if account.isSignedIn {
            items.append(.action("New Terminal Session") { model.startSession(account) })
            items.append(.action("Sign In Again…") { model.signIn(account) })
        } else {
            items.append(.action("Sign In…") { model.signIn(account) })
        }
        items.append(.separator)
        items.append(.action("Show Config Folder in Finder") { model.openFolder(account) })
        NativeMenu.popUp(items)
    }
}

struct LimitLine: View {
    let limit: Limit

    var body: some View {
        VStack(alignment: .leading, spacing: 2) {
            // Drawn rather than a ProgressView: AppKit's bar ignores tint.
            GeometryReader { proxy in
                Capsule()
                    .fill(.quaternary)
                    .overlay(alignment: .leading) {
                        Capsule()
                            .fill(tint)
                            .frame(width: limit.used > 0 ? max(proxy.size.width * fraction, 4) : 0)
                    }
            }
            .frame(height: 4)
            Text(caption)
                .font(.system(size: 10))
                .foregroundStyle(.tertiary)
                .monospacedDigit()
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(limit.shortLabel)
        .accessibilityValue(caption)
    }

    private var fraction: Double { Double(min(max(limit.used, 0), 100)) / 100 }

    private var caption: String {
        var text = "\(limit.shortLabel) \(limit.used)%"
        if let resets = limit.resets { text += " · resets \(resets)" }
        return text
    }

    private var tint: Color {
        switch limit.used {
        case ..<70: .green
        case ..<90: .orange
        default: .red
        }
    }
}

struct ErrorRow: View {
    let message: String
    let dismiss: () -> Void

    var body: some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            Image(systemName: "exclamationmark.triangle.fill")
                .symbolRenderingMode(.multicolor)
            Text(message)
                .font(.system(size: 11))
                .foregroundStyle(.secondary)
                .textSelection(.enabled)
                .fixedSize(horizontal: false, vertical: true)
            Spacer(minLength: 0)
            Button("Dismiss", systemImage: "xmark.circle.fill", action: dismiss)
                .labelStyle(.iconOnly)
                .buttonStyle(.borderless)
                .foregroundStyle(.tertiary)
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 4)
    }
}

// MARK: Menu building blocks

/// A full-width row with the system menu's hover highlight.
struct HighlightRow<Content: View>: View {
    let action: () -> Void
    @ViewBuilder let content: Content
    @State private var hovering = false
    @Environment(\.isEnabled) private var isEnabled

    var body: some View {
        Button(action: action) {
            content
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, 9)
                .padding(.vertical, 2)
                .contentShape(.rect)
        }
        .buttonStyle(.plain)
        .background {
            RoundedRectangle(cornerRadius: 5, style: .continuous)
                .fill(hovering && isEnabled ? Color.primary.opacity(0.1) : .clear)
        }
        .padding(.horizontal, 5)
        .onHover { hovering = $0 }
    }
}

/// A plain menu item: title, and a shortcut on the right.
struct MenuItemRow: View {
    let title: String
    var shortcut: String?
    let action: () -> Void
    @Environment(\.isEnabled) private var isEnabled

    var body: some View {
        HighlightRow(action: action) {
            HStack {
                Text(title)
                    .foregroundStyle(isEnabled ? .primary : .tertiary)
                Spacer()
                if let shortcut {
                    Text(shortcut)
                        .foregroundStyle(.tertiary)
                }
            }
            .font(.system(size: 13))
            .frame(height: 18)
        }
    }
}

/// A title with a switch, as in Control Center's menus.
struct ToggleRow: View {
    let title: String
    @Binding var isOn: Bool

    var body: some View {
        HStack {
            Text(title)
                .font(.system(size: 13))
            Spacer()
            Toggle(title, isOn: $isOn)
                .toggleStyle(.switch)
                .controlSize(.mini)
                .labelsHidden()
        }
        .padding(.horizontal, 14)
        .frame(height: 24)
    }
}

struct SectionHeader: View {
    let title: String

    var body: some View {
        Text(title)
            .font(.system(size: 11, weight: .semibold))
            .foregroundStyle(.secondary)
            .padding(.horizontal, 14)
            .padding(.top, 2)
            .padding(.bottom, 3)
    }
}

struct MenuSeparator: View {
    var body: some View {
        Divider()
            .padding(.horizontal, 14)
            .padding(.vertical, 5)
    }
}

/// Pops up a real NSMenu at the pointer, for the per-account actions.
enum NativeMenu {
    enum Item {
        case action(String, () -> Void)
        case separator
    }

    private final class Handler: NSObject {
        let run: () -> Void
        init(_ run: @escaping () -> Void) { self.run = run }
        @objc func runAction() { run() }
    }

    static func popUp(_ items: [Item]) {
        let menu = NSMenu()
        menu.autoenablesItems = false
        for item in items {
            switch item {
            case .separator:
                menu.addItem(.separator())
            case .action(let title, let run):
                let handler = Handler(run)
                let menuItem = NSMenuItem(title: title, action: #selector(Handler.runAction), keyEquivalent: "")
                menuItem.target = handler
                menuItem.representedObject = handler // keeps the handler alive
                menu.addItem(menuItem)
            }
        }
        menu.popUp(positioning: nil, at: NSEvent.mouseLocation, in: nil)
    }
}
