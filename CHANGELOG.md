# Changelog

## 0.1.0 - 2026-11-25

First release.

- `login`, `use` and `env` keep each Claude account in its own config folder (`~/.claude-<n>`) and start Claude Code as any of them, with no signing in again.
- `status` reads every account's usage live through Claude Code's own `/usage` command (no tokens spent, credentials never touched), and labels readings over two hours old as stale.
- `suggest` and `use best` pick the account with the most room left.
- Shell completions for bash, zsh and fish.
