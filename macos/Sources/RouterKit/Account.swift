import Foundation

/// One account as reported by `claude-router status --json`.
public struct Account: Decodable, Identifiable, Equatable, Sendable {
    public enum State: String, Decodable, Sendable {
        case ok
        case signedOut = "signed_out"
        case missing
        case unread
        case unknown

        public init(from decoder: Decoder) throws {
            let raw = try decoder.singleValueContainer().decode(String.self)
            self = State(rawValue: raw) ?? .unknown
        }
    }

    public var id: String
    public var folder: String
    public var state: State
    public var email: String?
    public var plan: String?
    public var limits: [Limit]
    public var ageSeconds: Int?
    /// Whether the reading is recent enough for claude-router to route on.
    public var fresh: Bool
    /// Percent left; nil unless the reading is fresh and has limits.
    public var room: Int?
    /// The account the next launched session starts on.
    public var next: Bool

    enum CodingKeys: String, CodingKey {
        case id = "account", folder, state, email, plan, limits
        case ageSeconds = "age_seconds", fresh, room, next
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        folder = try c.decodeIfPresent(String.self, forKey: .folder) ?? ""
        state = try c.decodeIfPresent(State.self, forKey: .state) ?? .unknown
        email = try c.decodeIfPresent(String.self, forKey: .email)
        plan = try c.decodeIfPresent(String.self, forKey: .plan)
        limits = try c.decodeIfPresent([Limit].self, forKey: .limits) ?? []
        ageSeconds = try c.decodeIfPresent(Int.self, forKey: .ageSeconds)
        fresh = try c.decodeIfPresent(Bool.self, forKey: .fresh) ?? false
        room = try c.decodeIfPresent(Int.self, forKey: .room)
        // Older claude-router versions don't report it.
        next = try c.decodeIfPresent(Bool.self, forKey: .next) ?? false
    }

    public var name: String { "claude\(id)" }

    public var isSignedIn: Bool { state == .ok }

    /// The plan as shown to people: "max" → "Max".
    public var planLabel: String? { plan.map { $0.prefix(1).uppercased() + $0.dropFirst() } }
}

public struct Limit: Decodable, Equatable, Sendable {
    public var label: String
    public var used: Int
    public var resets: String?

    public init(label: String, used: Int, resets: String? = nil) {
        self.label = label
        self.used = used
        self.resets = resets
    }

    /// "Current week (all models)" → "Week", "Current session" → "Session",
    /// "Current week (Sonnet only)" → "Week, Sonnet only".
    public var shortLabel: String {
        var text = label
        if text.hasPrefix("Current ") { text.removeFirst("Current ".count) }
        text = text.prefix(1).uppercased() + text.dropFirst()
        guard let open = text.firstIndex(of: "("), text.hasSuffix(")") else { return text }
        let base = text[..<open].trimmingCharacters(in: .whitespaces)
        let detail = text[text.index(after: open)..<text.index(before: text.endIndex)]
        return detail == "all models" ? base : "\(base), \(detail)"
    }
}

extension [Account] {
    /// The account the next session starts on, if claude-router named one.
    public var nextAccount: Account? { first(where: \.next) }
}

/// "just now", "5 min ago", "3 h ago", "2 days ago".
public func ageText(seconds: Int) -> String {
    switch seconds {
    case ..<60: "just now"
    case ..<3600: "\(seconds / 60) min ago"
    case ..<(48 * 3600): "\(seconds / 3600) h ago"
    default: "\(seconds / 86400) days ago"
    }
}
