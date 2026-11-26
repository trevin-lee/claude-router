package main

import (
	"fmt"
	"io"
)

// Account numbers are completed from `claude-router list`, so CLAUDE_ROUTER_ACCOUNTS applies.
const bashCompletion = `# bash completion for claude-router
_claude_router() {
  local cur=${COMP_WORDS[COMP_CWORD]}
  if [ "$COMP_CWORD" -eq 1 ]; then
    COMPREPLY=($(compgen -W "login use launch setup env status suggest list completion help version" -- "$cur"))
    return
  fi
  case ${COMP_WORDS[1]} in
    login|env) [ "$COMP_CWORD" -eq 2 ] && COMPREPLY=($(compgen -W "$(claude-router list 2>/dev/null | awk '{sub(/^claude/, "", $1); print $1}')" -- "$cur")) ;;
    use) [ "$COMP_CWORD" -eq 2 ] && COMPREPLY=($(compgen -W "best $(claude-router list 2>/dev/null | awk '{sub(/^claude/, "", $1); print $1}')" -- "$cur")) ;;
    status) COMPREPLY=($(compgen -W "--cached --json" -- "$cur")) ;;
    suggest) COMPREPLY=($(compgen -W "--cached" -- "$cur")) ;;
    completion) COMPREPLY=($(compgen -W "bash zsh fish" -- "$cur")) ;;
  esac
}
complete -F _claude_router claude-router
`

const zshCompletion = `#compdef claude-router
# zsh completion for claude-router
_claude_router() {
  local -a ids
  if (( CURRENT == 2 )); then
    _values 'command' \
      'login[sign an account in]' 'use[start Claude Code as an account]' \
      'launch[start claude on the account with the most room]' 'setup[share one setup across accounts]' \
      'env[print the export line]' 'status[sign-in and usage of each account]' \
      'suggest[account with the most room left]' 'list[accounts and folders]' \
      'completion[shell completion script]' 'help[show help]' 'version[show version]'
    return
  fi
  ids=(${(f)"$(claude-router list 2>/dev/null | awk '{sub(/^claude/, "", $1); print $1}')"})
  case $words[2] in
    login|env) (( CURRENT == 3 )) && _values 'account' $ids ;;
    use) (( CURRENT == 3 )) && _values 'account' best $ids ;;
    status) _values 'option' --cached --json ;;
    suggest) _values 'option' --cached ;;
    completion) _values 'shell' bash zsh fish ;;
  esac
}
_claude_router "$@"
`

const fishCompletion = `# fish completion for claude-router
function __claude_router_ids
    claude-router list 2>/dev/null | string replace -r '^claude(\S+).*' '$1'
end
complete -c claude-router -f
complete -c claude-router -n __fish_use_subcommand -a login -d 'Sign an account in'
complete -c claude-router -n __fish_use_subcommand -a use -d 'Start Claude Code as an account'
complete -c claude-router -n __fish_use_subcommand -a launch -d 'Start claude on the account with the most room'
complete -c claude-router -n __fish_use_subcommand -a setup -d 'Share one setup across accounts'
complete -c claude-router -n __fish_use_subcommand -a env -d 'Print the export line'
complete -c claude-router -n __fish_use_subcommand -a status -d 'Sign-in and usage of each account'
complete -c claude-router -n __fish_use_subcommand -a suggest -d 'Account with the most room left'
complete -c claude-router -n __fish_use_subcommand -a list -d 'Accounts and folders'
complete -c claude-router -n __fish_use_subcommand -a completion -d 'Shell completion script'
complete -c claude-router -n '__fish_seen_subcommand_from login env use' -a '(__claude_router_ids)'
complete -c claude-router -n '__fish_seen_subcommand_from use' -a best
complete -c claude-router -n '__fish_seen_subcommand_from status' -l cached -l json
complete -c claude-router -n '__fish_seen_subcommand_from suggest' -l cached
complete -c claude-router -n '__fish_seen_subcommand_from completion' -a 'bash zsh fish'
`

func cmdCompletion(args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("completion takes one shell: bash, zsh or fish")
	}
	switch args[0] {
	case "bash":
		fmt.Fprint(stdout, bashCompletion)
	case "zsh":
		fmt.Fprint(stdout, zshCompletion)
	case "fish":
		fmt.Fprint(stdout, fishCompletion)
	default:
		return fmt.Errorf("completion: unknown shell %q (bash, zsh or fish)", args[0])
	}
	return nil
}
