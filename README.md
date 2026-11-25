# claude-router

Stay signed in to several Claude accounts at once and switch between them, with live usage for each.

## Why

Claude Code ties usage limits to the account. When one account runs out, you currently sign in to another one. This tool keeps each account signed in separately so switching takes one command and nothing needs logging in again.

## How it works

Each account gets its own Claude Code config folder, chosen with the `CLAUDE_CONFIG_DIR` environment variable:

| Account | Config folder |
|---|---|
| claude0 | `~/.claude-0` |
| claude1 | `~/.claude-1` |
| claude2 | `~/.claude-2` |

Sign in to each folder once with `claude-router login <n>`. After that, `claude-router use <n>` starts Claude Code under account `<n>` with nothing to log in again.

Note: `CLAUDE_CONFIG_DIR` is not in Anthropic's documented environment variable list, so it could change. Check Anthropic's terms for using more than one account.

## Install

```sh
brew install trevin-lee/tap/claude-router
```

Homebrew builds it from source and installs shell completions; `brew upgrade` picks up new releases. Without Homebrew:

```sh
go install github.com/trevin-lee/claude-router/cmd/claude-router@latest
```

or download a binary from the [releases](https://github.com/trevin-lee/claude-router/releases). It needs Claude Code on your `PATH` and nothing else.

## Use

```sh
claude-router login 0          # once per account: creates ~/.claude-0 and signs it in
claude-router use 1            # start Claude Code as claude1
claude-router use 1 --resume   # any further arguments go to claude
claude-router status           # each account's sign-in and usage, read live
claude-router suggest          # print the account number with the most room left
claude-router use best         # start Claude Code as that account
eval "$(claude-router env 2)"  # point this shell at claude2
```

`status` prints one line per account:

```
claude0  a@example.com (max)  session 80% (resets 3pm) · week 40% (resets Nov 30 at 9am)  [read just now]
claude1  b@example.com (max)  week 60% (resets Nov 30 at 9am)  [read just now]
claude2  not signed in: run  claude-router login 2
```

Add `--cached` to `status` or `suggest` to skip the live read, and `--json` to `status` for scripts.

## How usage is read

For each signed-in account, the tool runs Claude Code's own `/usage` command in print mode (`claude -p /usage`) under that account's folder. This spends no tokens and leaves credentials to Claude Code. It runs from an empty temporary folder, with no MCP servers and no saved transcript, and takes about 15 seconds; all accounts are read at once.

Readings are kept in your user cache folder (`~/Library/Caches/claude-router` on macOS). A reading more than two hours old is labelled STALE and never used by `suggest`. If a live read fails, the last reading stays, marked as failed.

"Room left" is 100% minus the higher of the session and all-models weekly figures.

## Notes

- `claude-router use` starts the `claude` binary directly, skipping any `claude` shell function or alias. If your `claude` command is a wrapper that sets or clears `CLAUDE_CONFIG_DIR` itself, `eval "$(claude-router env <n>)"` won't stick; use `claude-router use <n>` instead.
- Each account folder starts empty: settings, skills, MCP servers and memory from `~/.claude` do not carry over.
- Settings: `CLAUDE_ROUTER_ACCOUNTS` (account numbers, default `0 1 2`), `CLAUDE_ROUTER_DIR_PREFIX` (default `~/.claude-`), `CLAUDE_ROUTER_CACHE` (where readings are kept), `CLAUDE_BIN` (default: `claude` on `PATH`).

## Develop

```sh
go test ./...
```

The end-to-end tests build the binary and run it against a fake `claude`; they never touch a real account.

Releasing: push a `vX.Y.Z` tag. The release workflow builds binaries with GoReleaser, publishes the GitHub release, and updates the formula in [trevin-lee/homebrew-tap](https://github.com/trevin-lee/homebrew-tap) from `homebrew/claude-router.rb.tmpl`. It needs a `TAP_DEPLOY_KEY` secret: the private half of a deploy key with write access to the tap.

## Security

The tool only points Claude Code at a config folder. It never reads, copies or prints credentials. Do not commit config folders or tokens.

## License

MIT. See [LICENSE](LICENSE).
