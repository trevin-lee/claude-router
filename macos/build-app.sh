#!/bin/bash
# Builds "Claude Router.app", the menu bar app, into macos/build/.
#   macos/build-app.sh [version]
# The version defaults to the latest tag. The app is ad-hoc signed, which is
# enough to run it on the machine that built it.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
version="${1:-$(git -C "$here" describe --tags --abbrev=0 2>/dev/null | sed 's/^v//')}"
version="${version:-0.0.0}"
app="$here/build/Claude Router.app"

swift build --package-path "$here" -c release --product ClaudeRouterBar
bin="$(swift build --package-path "$here" -c release --show-bin-path)/ClaudeRouterBar"

rm -rf "$app"
mkdir -p "$app/Contents/MacOS"
cp "$bin" "$app/Contents/MacOS/ClaudeRouterBar"
cat > "$app/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleIdentifier</key><string>io.github.trevin-lee.claude-router</string>
  <key>CFBundleName</key><string>Claude Router</string>
  <key>CFBundleDisplayName</key><string>Claude Router</string>
  <key>CFBundleExecutable</key><string>ClaudeRouterBar</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>$version</string>
  <key>CFBundleVersion</key><string>$version</string>
  <key>LSMinimumSystemVersion</key><string>14.0</string>
  <key>LSUIElement</key><true/>
  <key>LSApplicationCategoryType</key><string>public.app-category.developer-tools</string>
  <key>NSHumanReadableCopyright</key><string>MIT License</string>
</dict>
</plist>
EOF
codesign --force --sign - "$app" >/dev/null
echo "$app"
