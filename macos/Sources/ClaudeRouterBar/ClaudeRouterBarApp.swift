import RouterKit
import SwiftUI

@main
struct ClaudeRouterBarApp: App {
    @State private var model: RouterModel

    init() {
        let model = RouterModel()
        _model = State(initialValue: model)
        #if DEBUG
        Snapshot.runIfRequested(model: model)
        #endif
    }

    var body: some Scene {
        MenuBarExtra {
            PanelView(model: model)
        } label: {
            MenuBarLabel(next: model.nextAccount)
        }
        .menuBarExtraStyle(.window)
    }
}

/// The menu bar item: the next session's account and how much room it has.
struct MenuBarLabel: View {
    let next: Account?

    var body: some View {
        Group {
            if let next, let room = next.room {
                Label("\(next.id) · \(room)%", systemImage: "arrow.triangle.branch")
                    .labelStyle(.titleAndIcon)
            } else {
                Image(systemName: "arrow.triangle.branch")
            }
        }
        .accessibilityLabel(accessibilityText)
    }

    private var accessibilityText: String {
        guard let next, let room = next.room else { return "Claude Router" }
        return "Claude Router: next session on \(next.name), \(room) percent left"
    }
}
