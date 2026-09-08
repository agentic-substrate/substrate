package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/spf13/cobra"

	"github.com/agentic-substrate/substrate/internal/identity"
)

func newAuthCmd(d Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Store and inspect the credential this machine uses",
		Long: `auth login stores a user token in <config dir>/config.json at mode 0600.

The token is read from stdin, never from argv, so it does not reach the shell
history or another user's "ps" output. User tokens do not expire, so the file
is a long-lived credential: keep its mode 0600, and revoke with
"substrate token revoke" when the machine is retired.`,
	}
	cmd.AddCommand(newAuthLoginCmd(d), newAuthStatusCmd(d))
	return cmd
}

func newAuthLoginCmd(d Deps) *cobra.Command {
	var flags struct {
		server string
	}
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Read a user token from stdin and store it 0600",
		Long: `Reads one line from stdin, validates it against the server, and writes it to
<config dir>/config.json with mode 0600 via a temp file plus rename.

The token is never accepted as an argument: an argv token is visible to every
process on the machine and is recorded by the shell.`,
		Example: "  substrate auth login --server https://substrate.internal < token.txt\n  pbpaste | substrate auth login --server https://substrate.internal",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if flags.server == "" {
				return &UserError{
					What: "auth login needs a server",
					Why:  "neither --server nor SUBSTRATE_SERVER is set",
					Next: "run: substrate auth login --server https://substrate.example",
				}
			}
			token, err := readToken(d)
			if err != nil {
				return err
			}
			server := strings.TrimRight(flags.server, "/")
			if err := validateToken(cmd.Context(), d, server, token); err != nil {
				return err
			}
			path, err := saveConfig(d, Config{Server: server, Token: token})
			if err != nil {
				return err
			}
			// The token itself is never echoed back, here or anywhere else.
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "stored credential for %s in %s (mode 0600)\n", server, path)
			return err
		},
	}
	cmd.Flags().StringVar(&flags.server, "server", d.env("SUBSTRATE_SERVER"), "control plane base URL")
	return cmd
}

// readToken takes exactly one line from stdin. A nil Stdin is non-interactive
// and fails loudly rather than blocking a script forever.
func readToken(d Deps) (string, error) {
	if d.Stdin == nil {
		return "", &UserError{
			What: "auth login has no stdin to read the token from",
			Why:  "stdin is closed, and the token is deliberately not accepted as an argument",
			Next: "pipe it in: substrate auth login --server <url> < token.txt",
		}
	}
	line, err := bufio.NewReader(d.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("auth login: read token: %w", err)
	}
	token := strings.TrimSpace(line)
	if token == "" {
		return "", &UserError{
			What: "auth login read an empty token",
			Why:  "stdin held no non-blank line",
			Next: "pipe the token in: substrate auth login --server <url> < token.txt",
		}
	}
	if _, err := identity.DecodeToken(token); err != nil {
		return "", &UserError{
			What: "auth login rejected the token",
			Why:  err.Error(),
			Next: "mint one with: substrate admin create-user --dsn <dsn> --name <you> --org <org> --team <team>",
		}
	}
	return token, nil
}

// validateToken proves the token authenticates before it is written to disk.
// It calls GET /v1/render, which every principal may reach; this issue adds no
// server endpoint, so there is no introspection call that would name the
// principal back (see #97 constraint 3).
func validateToken(ctx context.Context, d Deps, server, token string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server+"/v1/render", nil) //nolint:gosec // G704: --server is the operator's own control plane
	if err != nil {
		return fmt.Errorf("auth login: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := d.httpClient().Do(req) //nolint:gosec // G704: --server is the operator's own control plane
	if err != nil {
		return &UserError{
			What: fmt.Sprintf("cannot reach %s", server),
			Why:  err.Error(),
			Next: "check --server and that the control plane is running: substrate doctor",
		}
	}
	defer func() { _ = res.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	// 401 and 403 are different problems and must not share advice. rest.go
	// maps every policy denial and every RLS refusal (Postgres 42501) to 403,
	// so a brand-new principal whose scope denies gets a 403 with a perfectly
	// good token: telling it to mint another sends it round the loop forever.
	if res.StatusCode == http.StatusUnauthorized {
		return &UserError{
			What: "the server rejected that token",
			Why:  fmt.Sprintf("GET %s/v1/render returned %s: the credential is unknown, malformed or revoked", server, res.Status),
			Next: "mint a fresh one with: substrate admin create-user --dsn <dsn> --name <you> --org <org> --team <team>",
		}
	}
	if res.StatusCode == http.StatusForbidden {
		return &UserError{
			What: "the token authenticated but the server denied the request",
			Why:  fmt.Sprintf("GET %s/v1/render returned %s: this is a permissions problem, not a token problem -- the scope, visibility or review policy refused", server, res.Status),
			Next: "ask an org admin for access to the scope you are working in, or pick one you hold: substrate context use --org <org> --team <team>",
		}
	}
	// 503 is requireStore: the control plane is up but its database is not.
	// The token was never examined, so refusing to store it would be a guess.
	if res.StatusCode == http.StatusServiceUnavailable {
		return &UserError{
			What: fmt.Sprintf("%s is not ready to validate a token", server),
			Why:  fmt.Sprintf("GET %s/v1/render returned %s: the server is reachable but its database is not, so the token was not checked either way", server, res.Status),
			Next: "wait for the server to become ready, then retry: substrate doctor",
		}
	}
	if res.StatusCode >= 400 {
		return &UserError{
			What: "the server did not accept the login probe",
			Why:  fmt.Sprintf("GET %s/v1/render returned %s", server, res.Status),
			Next: "check the server logs, then retry: substrate auth login --server " + server,
		}
	}
	return nil
}

// authStatus is the --json shape of `auth status`. It has no token field: the
// stored token must not appear in any output, log line or fixture.
type authStatus struct {
	Server      string `json:"server"`
	ConfigPath  string `json:"config_path"`
	TokenStored bool   `json:"token_stored"`
	Principal   string `json:"principal"`
}

func newAuthStatusCmd(d Deps) *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Short:   "Report the stored credential without printing it",
		Example: "  substrate auth status\n  substrate auth status --json",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, path, err := loadConfig(d)
			if errors.Is(err, ErrNoConfig) {
				return &UserError{
					What: "not logged in",
					Why:  fmt.Sprintf("%s does not exist", path),
					Next: "run: substrate auth login --server <url> < token.txt",
				}
			}
			if err != nil {
				return err
			}
			st := authStatus{
				Server:      cfg.Server,
				ConfigPath:  path,
				TokenStored: cfg.Token != "",
				// The server exposes no introspection endpoint yet and this
				// issue adds none, so the principal cannot be named here.
				Principal: "unknown (no server-side introspection endpoint yet)",
			}
			p := printer{w: cmd.OutOrStdout(), json: jsonFlag(cmd)}
			return p.emit(st, func(w io.Writer) error {
				stored := "absent"
				if st.TokenStored {
					stored = "stored (not shown)"
				}
				_, err := fmt.Fprintf(w, "server:    %s\nconfig:    %s\ntoken:     %s\nprincipal: %s\n",
					st.Server, st.ConfigPath, stored, st.Principal)
				return err
			})
		},
	}
}
