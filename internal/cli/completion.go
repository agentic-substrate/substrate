package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// ShellCompRequestCmd is the hidden cobra command a generated completion script
// calls back into to ask this binary for candidates. Naming it here lets the
// tests assert the scripts are driven by the real command tree.
const ShellCompRequestCmd = cobra.ShellCompRequestCmd

// completionFlags holds the flags for `substrate completion`. It lives in a
// closure struct rather than at package level so two roots built from
// different Deps stay independent (see NewRoot).
type completionFlags struct {
	noDescriptions bool
}

// newCompletionCmd builds `substrate completion bash|zsh|fish`, which writes a
// shell-completion script for the real command tree to stdout. Cobra generates
// the script by walking this root, so the completions cannot drift from the
// commands NewRoot actually registers -- nothing here is hand-written shell.
func newCompletionCmd(d Deps) *cobra.Command {
	f := &completionFlags{}
	cmd := &cobra.Command{
		Use:   "completion <bash|zsh|fish>",
		Short: "Write a shell completion script for substrate",
		Long: `Generates the completion script for one shell and writes it to stdout.

Load it for the current shell, or install it where your shell looks:

  bash:  source <(substrate completion bash)
  zsh:   substrate completion zsh > "${fpath[1]}/_substrate"
  fish:  substrate completion fish > ~/.config/fish/completions/substrate.fish

The script is generated from substrate's own command tree, so it always
matches the commands this binary exposes.`,
		Example: "  substrate completion bash\n  substrate completion fish > ~/.config/fish/completions/substrate.fish",
		Args:    cobra.ExactArgs(1),
		// The shells substrate supports; cobra also completes this list.
		ValidArgs: []string{"bash", "zsh", "fish"},
		RunE: func(cmd *cobra.Command, args []string) error {
			return writeCompletion(cmd.Root(), args[0], cmd.OutOrStdout(), !f.noDescriptions)
		},
	}
	cmd.Flags().BoolVar(&f.noDescriptions, "no-descriptions", false,
		"omit the per-completion description text from the generated script")
	_ = d // completion reads no environment, config or clock; Deps is taken for shape.
	return cmd
}

// writeCompletion renders root's completion script for shell to out. An
// unsupported shell is rejected loudly rather than silently emitting nothing.
func writeCompletion(root *cobra.Command, shell string, out io.Writer, descriptions bool) error {
	switch shell {
	case "bash":
		if err := root.GenBashCompletionV2(out, descriptions); err != nil {
			return fmt.Errorf("generate bash completion: %w", err)
		}
	case "zsh":
		var err error
		if descriptions {
			err = root.GenZshCompletion(out)
		} else {
			err = root.GenZshCompletionNoDesc(out)
		}
		if err != nil {
			return fmt.Errorf("generate zsh completion: %w", err)
		}
	case "fish":
		if err := root.GenFishCompletion(out, descriptions); err != nil {
			return fmt.Errorf("generate fish completion: %w", err)
		}
	default:
		return &UserError{
			What: fmt.Sprintf("unsupported shell %q", shell),
			Why:  "substrate generates completions for bash, zsh and fish only",
			Next: "substrate completion bash",
		}
	}
	return nil
}
