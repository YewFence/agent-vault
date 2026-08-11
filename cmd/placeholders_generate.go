package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/Infisical/agent-vault/internal/broker"
	"github.com/Infisical/agent-vault/internal/catalog"
)

// placeholdersGenerateCmd interactively masks one real credential into a
// credential-shaped placeholder. Unlike its parent it never touches the
// network, session, or vault — it is a pure local formatter.
var placeholdersGenerateCmd = &cobra.Command{
	Use:   "generate",
	Short: "Interactively mask a real credential into a credential-shaped placeholder",
	Long: `Interactively convert a real API key or token into a credential-shaped
placeholder of the same length: the vendor prefix, the self-documenting
marker "thisisaplaceholder", and zero padding.

The real credential is read with hidden input and never leaves the
terminal — no server connection is made. The prefix prompt is prefilled
with the longest catalog prefix matching the pasted key.

Standard output contains only the placeholder; notes and warnings go to
standard error, so the result is safe to capture with command substitution.`,
	Args: cobra.NoArgs,
	RunE: placeholdersGenerateRunE,
}

func placeholdersGenerateRunE(cmd *cobra.Command, _ []string) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return fmt.Errorf("generate is interactive-only and requires a terminal")
	}

	var realKey string
	err := huh.NewInput().
		Title("Paste the real API key or token (input is hidden):").
		EchoMode(huh.EchoModePassword).
		Value(&realKey).
		Validate(func(s string) error {
			if strings.TrimSpace(s) == "" {
				return fmt.Errorf("credential cannot be empty")
			}
			return nil
		}).
		Run()
	if err != nil {
		return abortGenerate(cmd, err)
	}
	realKey = strings.TrimSpace(realKey)

	// Prefill with the longest catalog prefix matching the pasted key; the
	// user edits or accepts. Empty is allowed — the bare padded marker is
	// still self-documenting.
	prefix := suggestPlaceholderPrefix(realKey)
	err = huh.NewInput().
		Title("Vendor prefix for the placeholder (e.g. sk-ant-, github_pat_):").
		Value(&prefix).
		Validate(func(s string) error {
			if strings.ContainsAny(strings.TrimSpace(s), " \t") {
				return fmt.Errorf("prefix cannot contain whitespace")
			}
			return nil
		}).
		Run()
	if err != nil {
		return abortGenerate(cmd, err)
	}
	prefix = strings.TrimSpace(prefix)

	placeholder := maskCredential(prefix, len(realKey))
	if prefix != "" && !strings.HasPrefix(realKey, prefix) {
		fmt.Fprintf(cmd.ErrOrStderr(), "%s real credential does not start with prefix %q\n", warningText("Warning:"), prefix)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "%s\n", mutedText(fmt.Sprintf("%d chars — same length as the real credential", len(placeholder))))
	_, err = fmt.Fprintln(cmd.OutOrStdout(), placeholder)
	return err
}

// abortGenerate renders a friendly abort note on stderr — stdout stays
// placeholder-only — and swallows huh.ErrUserAborted.
func abortGenerate(cmd *cobra.Command, err error) error {
	if err != nil && errors.Is(err, huh.ErrUserAborted) {
		fmt.Fprintln(cmd.ErrOrStderr(), mutedText("Aborted."))
		return nil
	}
	return err
}

// maskCredential renders prefix + marker, zero-padded to keyLen so format
// checks keyed on the real credential's length still pass. It mirrors the
// padded branch of broker.GeneratePlaceholder; keyLen <= len(base) means
// no padding, and an empty prefix yields the bare padded marker.
func maskCredential(prefix string, keyLen int) string {
	base := prefix + broker.PlaceholderMarker
	if keyLen <= len(base) {
		return base
	}
	return base + strings.Repeat("0", keyLen-len(base))
}

// suggestPlaceholderPrefix returns the longest catalog PlaceholderPrefix
// realKey starts with (longest wins so "sk-or-…" suggests "sk-or-", not
// "sk-"), or "" when nothing matches.
func suggestPlaceholderPrefix(realKey string) string {
	best := ""
	for _, tpl := range catalog.GetAll() {
		p := tpl.PlaceholderPrefix
		if p != "" && len(p) > len(best) && strings.HasPrefix(realKey, p) {
			best = p
		}
	}
	return best
}

func init() {
	placeholdersCmd.AddCommand(placeholdersGenerateCmd)
}
