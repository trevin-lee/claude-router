# Changelog

## 0.3.0 - 2026-11-27

- A menu bar app for macOS, Claude Router (build it from source with `macos/build-app.sh`): shows the account new sessions will start on and its room left in the menu bar, and opens to every account's usage with reset times, reading it live each time it opens. From there you can start a terminal session or sign in as any account, sync the shared setup, and open the routing log. It runs claude-router itself, so it always agrees with what `launch` will do.
- `status --json` marks the account the next session will start on with `"next": true`.

## 0.2.0 - 2026-11-25

Several accounts now work as one.

- `launch` starts Claude Code on the account with the most room left, deciding instantly from the usage Claude Code records, and refreshes old readings in the background. Point a `claude` shell function or the VS Code process wrapper at it.
- Every account shares one setup with `~/.claude`: settings, skills, plugins, hooks, conversation history and memory are linked, and MCP servers, trusted folders and preferences are synced, so a change under any account reaches all of them. `setup` does this for every account; `login`, `use` and `launch` do it for theirs.
- `status` marks the account the next session will start on, and uses Claude Code's own usage record when it is newer.
- Fixed: a `claude` started from inside a Claude Code session used that session's login instead of the account's, so `status` and `use` could report or run the wrong account.
- Fixed: every usage read left an empty project behind in the account folder. Live reads now run from one folder of their own without loading settings, hooks or plugins, and the leftovers are removed.

## 0.1.0 - 2026-11-25

First release.

- `login`, `use` and `env` keep each Claude account in its own config folder (`~/.claude-<n>`) and start Claude Code as any of them, with no signing in again.
- `status` reads every account's usage live through Claude Code's own `/usage` command (no tokens spent, credentials never touched), and labels readings over two hours old as stale.
- `suggest` and `use best` pick the account with the most room left.
- Shell completions for bash, zsh and fish.
