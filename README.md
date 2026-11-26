# claude-router

Use several Claude accounts as if they were one. Each new Claude Code session starts on whichever account has the most room left, and every account shares the same settings, skills, plugins, MCP servers, conversation history and memory.

## Why

Claude Code ties usage limits to the account. When one account runs out, you normally sign out and in again, and the other account has none of your setup. claude-router keeps every account signed in at once, gives them one shared setup, and picks the account for you.

## How it works

Each account gets its own Claude Code config folder, chosen with the `CLAUDE_CONFIG_DIR` environment variable. Claude Code keeps the login for each folder separately in the system keychain, so all of them stay signed in:

| Account | Config folder |
|---|---|
| claude0 | `~/.claude-0` |
| claude1 | `~/.claude-1` |
| claude2 | `~/.claude-2` |

Everything that isn't the login is shared with your normal `~/.claude`:

- **Linked** into each account folder: `settings.json`, `CLAUDE.md`, skills, agents, commands, hooks, plugins, output styles, the status line, `projects/` (conversation history and memory), prompt history, plans, todos, file history and IDE connections.
- **Synced** in each account's `.claude.json` (which also holds account details, so it can't be linked): MCP servers, each folder's trust and allowed tools, and display preferences. A server added or removed under any account reaches all of them.

Because history is shared, `/resume` and the VS Code conversation list show every conversation, whichever account it ran on.

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

## Set up

Sign in each account once. Each opens a browser sign-in, then links the folder to your setup:

```sh
claude-router login 0
claude-router login 1
claude-router login 2
```

Then make `claude` start on the best account by itself. In your shell profile:

```sh
claude() { claude-router launch "$(whence -p claude)" "$@"; }   # zsh; in bash: "$(type -P claude)"
```

For the VS Code extension, set **Claude Code: Claude Process Wrapper** (`claudeCode.claudeProcessWrapper`) to a script that runs `exec claude-router launch "$@"`. `launch` prints nothing of its own, and if anything goes wrong it starts Claude Code as if claude-router weren't there.

## Use

```sh
claude-router status           # each account's sign-in and usage, read live; marks the next session's account
claude-router use 1            # start Claude Code as claude1
claude-router use best         # start Claude Code on the account with the most room
claude-router suggest          # print that account's number
claude-router setup            # re-link every account and sync shared settings (launch and use do this too)
eval "$(claude-router env 2)"  # point this shell at claude2
```

`status` prints one line per account:

```
claude0  a@example.com (max)  week 21% (resets Dec 1 at 3am)  [read just now]
claude1  b@example.com (max)  week 0% (resets Dec 2 at 12am)  [read just now]  <- next session
claude2  c@example.com (max)  week 100% (resets Nov 30 at 9am)  [read just now]
```

Add `--cached` to `status` or `suggest` to skip the live read, and `--json` to `status` for scripts.

## How usage is read

Claude Code records each account's usage in that account's `.claude.json`. `launch` reads that record, so it decides instantly: the account with the most room left, a limit whose reset time has passed counting as empty. When the readings are over 30 minutes old, it refreshes them in the background for next time.

`status` reads live: for each signed-in account it runs Claude Code's own `/usage` command in print mode (`claude -p /usage`). That spends no tokens and leaves credentials to Claude Code. It runs from a folder of its own, with no settings, hooks, plugins or MCP servers loaded and no transcript saved, and takes about 10 seconds; all accounts are read at once. A reading more than two hours old is labelled STALE and never used by `suggest`.

"Room left" is 100% minus the higher of the session and all-models weekly figures.

## Limits

- **A running conversation stays on its account.** When an account runs out mid-conversation, start a new session or `/resume` the conversation; it opens on the next account with the same history.
- **Some sign-ins belong to the account:** remote MCP servers that use OAuth, and claude.ai connectors, may need connecting once per account.
- **The usage format isn't documented.** If Claude Code changes how it records usage, `launch` falls back to the first signed-in account and `status` shows "no plan limits reported".
- **Sharing needs symbolic links**, so on Windows each account keeps its own setup.

## Notes

- `claude-router` drops the variables a running Claude Code session sets for its child processes. A `claude` started with them would attach to that session and use its login instead of the account's.
- If an account folder already has its own copy of something it would link, claude-router merges it into `~/.claude` where nothing clashes and sets the rest aside as `<name>.before-claude-router`. It never deletes your data.
- `launch` writes what it did to `router.log` in its cache folder (`~/Library/Caches/claude-router` on macOS).
- Settings: `CLAUDE_ROUTER_ACCOUNTS` (account numbers, default `0 1 2`), `CLAUDE_ROUTER_DIR_PREFIX` (default `~/.claude-`), `CLAUDE_ROUTER_HOME` (the shared folder, default `~/.claude`), `CLAUDE_ROUTER_CACHE` (readings and log), `CLAUDE_BIN` (default: `claude` on `PATH`).

## Develop

```sh
go test ./...
```

The end-to-end tests build the binary and run it against a fake `claude`, with a temporary `HOME`; they never touch a real account or your `~/.claude`.

Releasing: add a CHANGELOG entry and push a `vX.Y.Z` tag. The release workflow builds binaries with GoReleaser, publishes the GitHub release, and updates the formula in [trevin-lee/homebrew-tap](https://github.com/trevin-lee/homebrew-tap) from `homebrew/claude-router.rb.tmpl`. It needs a `TAP_DEPLOY_KEY` secret: the private half of a deploy key with write access to the tap.

## Security

claude-router only points Claude Code at config folders and links and syncs the parts of them that aren't the login. It never reads, copies or prints credentials, which stay in the system keychain. Do not commit config folders or tokens.

## License

MIT. See [LICENSE](LICENSE).
