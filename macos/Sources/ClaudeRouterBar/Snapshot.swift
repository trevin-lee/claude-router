#if DEBUG
import AppKit
import SwiftUI

/// Debug builds only: `CLAUDE_ROUTER_BAR_SNAPSHOT=out.png` draws the panel
/// offscreen once the accounts have loaded, saves it, and quits. Used to check
/// the layout without opening the menu bar.
enum Snapshot {
    static func runIfRequested(model: RouterModel) {
        guard let path = ProcessInfo.processInfo.environment["CLAUDE_ROUTER_BAR_SNAPSHOT"] else { return }
        let dark = ProcessInfo.processInfo.environment["CLAUDE_ROUTER_BAR_DARK"] == "1"
        Task {
            await model.loadCached()
            let host = NSHostingView(rootView: PanelView(model: model)
                .background(.regularMaterial))
            host.appearance = NSAppearance(named: dark ? .darkAqua : .aqua)
            let window = NSWindow(contentRect: .zero, styleMask: [.borderless], backing: .buffered, defer: false)
            window.contentView = host
            window.setContentSize(host.fittingSize)
            window.orderFrontRegardless()
            try? await Task.sleep(for: .seconds(1))
            host.layoutSubtreeIfNeeded()
            if let rep = host.bitmapImageRepForCachingDisplay(in: host.bounds) {
                host.cacheDisplay(in: host.bounds, to: rep)
                try? rep.representation(using: .png, properties: [:])?.write(to: URL(filePath: path))
            }
            NSApp.terminate(nil)
        }
    }
}
#endif
