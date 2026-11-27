import AppKit
import Observation
import RouterKit
import os
import ServiceManagement

private let log = Logger(subsystem: "io.github.trevin-lee.claude-router", category: "panel")

/// What the menu bar shows, kept current from claude-router.
@Observable
final class RouterModel {
    private(set) var accounts: [Account] = []
    private(set) var isReadingLive = false
    private(set) var isSettingUp = false
    private(set) var error: String?
    private(set) var loadedOnce = false

    private let router = Router.find()

    init() {
        Task { await keepCurrent() }
    }

    var nextAccount: Account? { accounts.nextAccount }

    /// Cheap reads every minute, so the menu bar figure follows real use.
    private func keepCurrent() async {
        while !Task.isCancelled {
            await loadCached()
            try? await Task.sleep(for: .seconds(60))
        }
    }

    func loadCached() async {
        guard let router else { return fail(Router.Failure.notInstalled) }
        do {
            accounts = try await router.status(live: false)
            error = nil
        } catch {
            fail(error)
        }
        loadedOnce = true
    }

    /// Called every time the panel opens: show what's known at once, then
    /// read every account live (unless a read is already running).
    func panelOpened() {
        log.info("panel opened")
        Task {
            await loadCached()
            await readLive()
        }
    }

    func readLive() async {
        guard let router, !isReadingLive else { return }
        isReadingLive = true
        defer { isReadingLive = false }
        do {
            accounts = try await router.status(live: true)
            error = nil
        } catch {
            fail(error)
        }
    }

    func runSetup() async {
        guard let router, !isSettingUp else { return }
        isSettingUp = true
        defer { isSettingUp = false }
        do {
            try await router.setup()
            error = nil
        } catch {
            fail(error)
        }
    }

    // MARK: Terminal actions

    /// Starts an interactive Claude Code session as this account.
    func startSession(_ account: Account) {
        guard let router else { return }
        openInTerminal(router.command("use", account.id), title: "Claude Code (\(account.name))")
    }

    /// Signs the account in; it needs a browser and possibly a prompt, so it
    /// runs in a terminal like it would by hand.
    func signIn(_ account: Account) {
        guard let router else { return }
        openInTerminal(router.command("login", account.id), title: "Sign in \(account.name)")
    }

    func openFolder(_ account: Account) {
        NSWorkspace.shared.open(URL(filePath: account.folder))
    }

    func openLog() {
        let log = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask)[0]
            .appending(path: "claude-router/router.log")
        if FileManager.default.fileExists(atPath: log.path) {
            NSWorkspace.shared.open(log)
        } else {
            error = "No sessions have been routed yet, so there's no log."
        }
    }

    /// Opens a new Terminal window running `command`, through a .command
    /// file, which needs no automation permission.
    private func openInTerminal(_ command: String, title: String) {
        let script = """
        #!/bin/zsh -l
        printf '\\e]0;%s\\a' \(shellQuoted(title))
        cd ~
        \(command)
        """
        do {
            let dir = FileManager.default.temporaryDirectory.appending(path: "claude-router-bar")
            try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
            let file = dir.appending(path: "\(UUID().uuidString).command")
            try script.write(to: file, atomically: true, encoding: .utf8)
            try FileManager.default.setAttributes([.posixPermissions: 0o700], ofItemAtPath: file.path)
            NSWorkspace.shared.open(file)
        } catch {
            fail(error)
        }
    }

    // MARK: Launch at login

    var launchesAtLogin: Bool {
        get { SMAppService.mainApp.status == .enabled }
        set {
            do {
                if newValue {
                    try SMAppService.mainApp.register()
                } else {
                    try SMAppService.mainApp.unregister()
                }
            } catch {
                fail(error)
            }
        }
    }

    private func fail(_ error: any Error) {
        self.error = error.localizedDescription
    }

    func dismissError() { error = nil }
}
