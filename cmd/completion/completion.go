// Package completion provides shell completion script generation.
package completion

import (
	"fmt"

	"github.com/urfave/cli/v2"
)

const bashScript = `# bash completion for createos
_createos_completion() {
    local cur="${COMP_WORDS[COMP_CWORD]}"
    local completions
    completions=$(createos --generate-bash-completion "${COMP_WORDS[@]:1}" 2>/dev/null)
    COMPREPLY=( $(compgen -W "$completions" -- "$cur") )
    return 0
}
complete -F _createos_completion createos`

const zshScript = `# zsh completion for createos
#compdef createos

_createos() {
    local -a completions
    completions=("${(@f)$(${words[1]} --generate-bash-completion ${words[2,-1]} 2>/dev/null)}")
    compadd -a completions
}

_createos "$@"`

const fishScript = `# fish completion for createos
function __createos_complete
    set -l args (commandline -opc)
    set -e args[1]
    createos --generate-bash-completion $args
end

complete -f -c createos -a "(__createos_complete)"`

// NewCompletionCommand returns the shell completion command.
func NewCompletionCommand() *cli.Command {
	return &cli.Command{
		Name:      "completion",
		Usage:     "Generate shell completion script",
		ArgsUsage: "<bash|zsh|fish>",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "shell", Usage: "Shell type: bash, zsh, or fish"},
		},
		Description: "Generate a shell completion script and load it into your shell.\n\n" +
			"   Bash — add to ~/.bashrc:\n" +
			"     echo 'source <(createos completion bash)' >> ~/.bashrc\n" +
			"     source ~/.bashrc\n\n" +
			"   Zsh — add to ~/.zshrc:\n" +
			"     echo 'source <(createos completion zsh)' >> ~/.zshrc\n" +
			"     source ~/.zshrc\n\n" +
			"   Fish — add to fish config:\n" +
			"     createos completion fish > ~/.config/fish/completions/createos.fish\n\n" +
			"   To load once for the current session only (without persisting):\n" +
			"     source <(createos completion bash)\n" +
			"     source <(createos completion zsh)",
		Action: func(c *cli.Context) error {
			shell := c.String("shell")
			if shell == "" {
				shell = c.Args().First()
			}
			switch shell {
			case "bash":
				fmt.Println(bashScript)
			case "zsh":
				fmt.Println(zshScript)
			case "fish":
				fmt.Println(fishScript)
			case "":
				return fmt.Errorf("please specify a shell\n\n  Supported shells: bash, zsh, fish\n\n  Example:\n    createos completion zsh")
			default:
				return fmt.Errorf("unsupported shell %q\n\n  Supported shells: bash, zsh, fish", shell)
			}
			return nil
		},
	}
}
