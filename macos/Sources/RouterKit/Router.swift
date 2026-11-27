import Foundation

/// Runs the claude-router command line tool. The app never reads account
/// files itself: everything goes through the same command VS Code and the
/// terminal use, so they always agree.
public struct Router: Sendable {
    public enum Failure: Error, LocalizedError, Equatable {
        case notInstalled
        case failed(String)

        public var errorDescription: String? {
            switch self {
            case .notInstalled:
                "claude-router isn't installed. Install it with: brew install trevin-lee/tap/claude-router"
            case .failed(let message):
                message
            }
        }
    }

    public let executable: URL

    public init(executable: URL) {
        self.executable = executable
    }

    /// Finds claude-router where Homebrew or `go install` put it. Apps don't
    /// get the shell's PATH, so the usual places are checked directly.
    /// `CLAUDE_ROUTER_BIN` overrides the search.
    public static func find(home: URL = FileManager.default.homeDirectoryForCurrentUser,
                            environment: [String: String] = ProcessInfo.processInfo.environment) -> Router? {
        let candidates = [
            environment["CLAUDE_ROUTER_BIN"] ?? "",
            "/opt/homebrew/bin/claude-router",
            "/usr/local/bin/claude-router",
            home.appending(path: "go/bin/claude-router").path,
            home.appending(path: ".local/bin/claude-router").path,
        ]
        return candidates
            .first { FileManager.default.isExecutableFile(atPath: $0) }
            .map { Router(executable: URL(filePath: $0)) }
    }

    /// The PATH given to claude-router, so it can find `claude`.
    public static func searchPath(home: URL = FileManager.default.homeDirectoryForCurrentUser) -> String {
        [
            home.appending(path: ".local/bin").path,
            "/opt/homebrew/bin", "/usr/local/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin",
        ].joined(separator: ":")
    }

    /// Every account's status. `live` reads usage through Claude Code (about
    /// 15 seconds, no tokens spent); otherwise the last readings are used.
    public func status(live: Bool) async throws -> [Account] {
        var args = ["status", "--json"]
        if !live { args.insert("--cached", at: 1) }
        let output = try await run(args)
        return try JSONDecoder().decode([Account].self, from: output)
    }

    /// Re-links every account and syncs shared settings.
    public func setup() async throws {
        _ = try await run(["setup"])
    }

    /// The command line that starts an interactive session, for a terminal.
    public func command(_ args: String...) -> String {
        ([executable.path] + args).map(shellQuoted).joined(separator: " ")
    }

    func run(_ args: [String]) async throws -> Data {
        let executable = executable
        return try await withCheckedThrowingContinuation { continuation in
            // Process and its pipes block, so they get a thread of their own
            // rather than one from the cooperative pool.
            Thread.detachNewThread {
                continuation.resume(with: Result { try Router.runBlocking(executable, args) })
            }
        }
    }

    static func runBlocking(_ executable: URL, _ args: [String]) throws -> Data {
        let process = Process()
        process.executableURL = executable
        process.arguments = args
        var env = ProcessInfo.processInfo.environment
        env["PATH"] = searchPath()
        process.environment = env
        let out = Pipe(), err = Pipe()
        process.standardOutput = out
        process.standardError = err
        process.standardInput = FileHandle.nullDevice
        try process.run()
        // Drain stderr alongside stdout so neither pipe can fill and stall it.
        nonisolated(unsafe) var errData = Data()
        let errDone = DispatchSemaphore(value: 0)
        Thread.detachNewThread {
            errData = err.fileHandleForReading.readDataToEndOfFile()
            errDone.signal()
        }
        let outData = out.fileHandleForReading.readDataToEndOfFile()
        errDone.wait()
        process.waitUntilExit()
        guard process.terminationStatus == 0 else {
            let message = String(decoding: errData, as: UTF8.self)
                .trimmingCharacters(in: .whitespacesAndNewlines)
            throw Failure.failed(message.isEmpty
                ? "claude-router \(args.first ?? "") exited with status \(process.terminationStatus)"
                : message)
        }
        return outData
    }
}

/// Quotes a word for zsh/bash.
public func shellQuoted(_ word: String) -> String {
    let safe = word.allSatisfy { $0.isLetter || $0.isNumber || "/-_.:=@%+,".contains($0) }
    if safe && !word.isEmpty { return word }
    return "'" + word.replacingOccurrences(of: "'", with: "'\\''") + "'"
}
