package app

import (
	"fmt"
	"io"
)

func printCompletion(w io.Writer, shell string) error {
	switch shell {
	case "bash":
		fmt.Fprint(w, bashCompletion)
	case "zsh":
		fmt.Fprint(w, zshCompletion)
	case "fish":
		fmt.Fprint(w, fishCompletion)
	default:
		return fmt.Errorf("unsupported shell: %s (supported: bash, zsh, fish)", shell)
	}
	return nil
}

const bashCompletion = `_ember() {
    local cur opts
    cur="${COMP_WORDS[COMP_CWORD]}"
    opts="--addr --interval --slow-threshold --pid --json --expose --daemon --no-color --version --help --completion"
    COMPREPLY=($(compgen -W "${opts}" -- "${cur}"))
}
complete -F _ember ember
`

const zshCompletion = `#compdef ember

_ember() {
    _arguments \
        '--addr[Caddy admin API address]:addr:' \
        '--interval[Polling interval]:interval:' \
        '--slow-threshold[Slow request threshold in ms]:threshold:' \
        '--pid[FrankenPHP PID]:pid:' \
        '--json[JSON output mode]' \
        '--expose[Expose Prometheus metrics]:addr:' \
        '--daemon[Headless mode]' \
        '--no-color[Disable colors]' \
        '--version[Show version]' \
        '--completion[Generate shell completions]:shell:(bash zsh fish)' \
        '--help[Show help]'
}

_ember "$@"
`

const fishCompletion = `complete -c ember -l addr -d 'Caddy admin API address'
complete -c ember -l interval -d 'Polling interval'
complete -c ember -l slow-threshold -d 'Slow request threshold in ms'
complete -c ember -l pid -d 'FrankenPHP PID'
complete -c ember -l json -d 'JSON output mode'
complete -c ember -l expose -d 'Expose Prometheus metrics'
complete -c ember -l daemon -d 'Headless mode'
complete -c ember -l no-color -d 'Disable colors'
complete -c ember -l version -d 'Show version'
complete -c ember -l completion -d 'Generate shell completions' -xa 'bash zsh fish'
complete -c ember -l help -d 'Show help'
`
