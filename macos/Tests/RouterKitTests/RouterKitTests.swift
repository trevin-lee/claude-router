import Foundation
import Testing
@testable import RouterKit

private let sample = Data("""
[
  {"account": "0", "folder": "/h/.claude-0", "state": "ok", "email": "a@example.com", "plan": "max",
   "limits": [{"label": "Current week (all models)", "used": 38, "resets": "Dec 1 at 3am", "resets_at": 1796122800}],
   "read_epoch": 1795690350, "age_seconds": 361, "fresh": true, "room": 62, "next": false},
  {"account": "1", "folder": "/h/.claude-1", "state": "ok", "email": "b@example.com", "plan": "max",
   "limits": [{"label": "Current session", "used": 4}, {"label": "Current week (all models)", "used": 0}],
   "age_seconds": 9000, "fresh": false, "room": null, "next": true},
  {"account": "2", "folder": "/h/.claude-2", "state": "signed_out", "age_seconds": null, "fresh": false, "room": null},
  {"account": "3", "folder": "/h/.claude-3", "state": "something_new", "age_seconds": null, "fresh": false, "room": null}
]
""".utf8)

@Test func `decodes claude-router status`() throws {
    let accounts = try JSONDecoder().decode([Account].self, from: sample)
    try #require(accounts.count == 4)
    #expect(accounts[0].name == "claude0")
    #expect(accounts[0].room == 62)
    #expect(accounts[0].planLabel == "Max")
    #expect(accounts[0].limits.first?.resets == "Dec 1 at 3am")
    #expect(accounts[1].limits.count == 2)
    #expect(accounts[2].state == .signedOut)
    #expect(accounts[2].limits.isEmpty)
    #expect(accounts[3].state == .unknown)
}

@Test func `next account`() throws {
    let accounts = try JSONDecoder().decode([Account].self, from: sample)
    #expect(accounts.nextAccount?.id == "1")
}

@Test func `older claude-router without next field`() throws {
    let data = Data(#"[{"account": "0", "state": "ok", "age_seconds": 5, "fresh": true, "room": 10}]"#.utf8)
    let accounts = try JSONDecoder().decode([Account].self, from: data)
    #expect(accounts.nextAccount == nil)
}

@Test(arguments: [
    ("Current week (all models)", "Week"),
    ("Current session", "Session"),
    ("Current week (Sonnet only)", "Week, Sonnet only"),
    ("Current week (Opus)", "Week, Opus"),
    ("Extra usage", "Extra usage"),
])
func `short limit labels`(label: String, expected: String) {
    #expect(Limit(label: label, used: 0).shortLabel == expected)
}

@Test(arguments: [(5, "just now"), (300, "5 min ago"), (7200, "2 h ago"), (3 * 86400, "3 days ago")])
func `age text`(seconds: Int, expected: String) {
    #expect(ageText(seconds: seconds) == expected)
}

@Test(arguments: [
    ("/opt/homebrew/bin/claude-router", "/opt/homebrew/bin/claude-router"),
    ("use", "use"),
    ("it's here", "'it'\\''s here'"),
    ("", "''"),
])
func `shell quoting`(word: String, expected: String) {
    #expect(shellQuoted(word) == expected)
}

@Test func `runs a command and reports its error`() async throws {
    let router = Router(executable: URL(filePath: "/bin/sh"))
    let out = try await router.run(["-c", "printf hi"])
    #expect(String(decoding: out, as: UTF8.self) == "hi")
    await #expect(throws: Router.Failure.failed("boom")) {
        try await router.run(["-c", "echo boom >&2; exit 3"])
    }
}
