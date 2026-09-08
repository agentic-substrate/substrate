package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"net/url"
	"path/filepath"
	"strings"

	"github.com/agentic-substrate/substrate/internal/adapter"
	"github.com/agentic-substrate/substrate/internal/cutover"
	"github.com/spf13/cobra"
)

func newAdapterCmd(d Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "adapter",
		Short: "Install, inspect, and remove the per-machine adapter",
	}
	cmd.AddCommand(newAdapterInstallCmd(d), newAdapterStatusCmd(d), newAdapterUninstallCmd(d))
	return cmd
}

// adapterCutoverFlags are shared by install and status: status is exactly the
// install classification with nothing written, which is why a preview is now
// a verb rather than a flag that exits 0 on stdout.
type adapterCutoverFlags struct {
	roots   []string
	server  string
	token   string
	machine string
	binary  string
	yes     bool
}

func (f *adapterCutoverFlags) bind(cmd *cobra.Command, withYes bool) {
	cmd.Flags().StringArrayVar(&f.roots, "root", nil, "absolute tree whose harness files are displaced (repeatable; defaults to /work; no $HOME default)")
	cmd.Flags().StringVar(&f.server, "server", "", "control plane base URL (default $SUBSTRATE_URL)")
	cmd.Flags().StringVar(&f.token, "token", "", "bearer token (default $SUBSTRATE_TOKEN)")
	cmd.Flags().StringVar(&f.machine, "machine", "", "machine name sent as GET /v1/render?machine=")
	cmd.Flags().StringVar(&f.binary, "binary", "substrate-adapter", "adapter binary written into the unit")
	if withYes {
		cmd.Flags().BoolVar(&f.yes, "yes", false, "skip the confirmation prompt; required when stdin is not a terminal")
	}
}

func newAdapterInstallCmd(d Deps) *cobra.Command {
	var f adapterCutoverFlags
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Displace harness files with rendered context and install the adapter unit (this commits)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCutover(cmd.Context(), d, &f, true)
		},
	}
	f.bind(cmd, true)
	return cmd
}

func newAdapterStatusCmd(d Deps) *cobra.Command {
	var f adapterCutoverFlags
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Classify what install would displace; writes nothing",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCutover(cmd.Context(), d, &f, false)
		},
	}
	f.bind(cmd, false)
	return cmd
}

func runCutover(ctx context.Context, d Deps, f *adapterCutoverFlags, commit bool) error {
	op := "adapter status"
	if commit {
		op = "adapter install"
	}
	roots, err := absRoots(op, f.roots)
	if err != nil {
		return err
	}
	if f.machine == "" {
		return &UserError{op + ": no --machine", "the render is machine-specific", "re-run with --machine $(hostname)"}
	}
	api, err := newAPIClient(d, op, f.server, f.token)
	if err != nil {
		return err
	}
	targets, err := fetchRenderTargets(ctx, api, f.machine)
	if err != nil {
		return err
	}
	home := roots[0]
	checkouts, err := adapter.Discover(roots)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	checkoutPaths := make([]string, 0, len(checkouts))
	for _, c := range checkouts {
		checkoutPaths = append(checkoutPaths, c.Path)
	}
	var files []cutover.Replacement
	for _, tgt := range targets {
		dests, err := cutover.Destinations(home, checkoutPaths, tgt.Path)
		if err != nil {
			return fmt.Errorf("%s: %w", op, err)
		}
		for _, dest := range dests {
			files = append(files, cutover.Replacement{Path: dest, Content: tgt.Content})
		}
	}
	if len(files) == 0 {
		return &UserError{
			What: op + ": nothing to displace",
			Why:  "GET /v1/render returned no paths under " + strings.Join(roots, ","),
			Next: "check --machine and --root, then re-run substrate adapter status",
		}
	}
	if commit {
		if err := confirm(d, op, f.yes); err != nil {
			return err
		}
	}
	rep, err := cutover.Cutover(cutover.Request{
		Roots:     roots,
		Home:      home,
		Commit:    commit,
		Installer: d.installer(),
		Files:     files,
		Server:    api.base,
		Token:     api.token,
		Machine:   f.machine,
		Binary:    f.binary,
	})
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return printReport(d, op, rep.Format())
}

func newAdapterUninstallCmd(d Deps) *cobra.Command {
	var f struct {
		roots []string
		force bool
		yes   bool
	}
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Restore every displaced *.pre-substrate file and remove the unit (this commits)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			const op = "adapter uninstall"
			// Uninstall always restores: there was never a non-restore path,
			// so the old -restore flag only existed because -commit defaulted
			// off. Dropping files without putting them back is not offered.
			roots, err := absRoots(op, f.roots)
			if err != nil {
				return err
			}
			if err := confirm(d, op, f.yes); err != nil {
				return err
			}
			rep, err := cutover.Restore(cutover.Request{
				Roots:     roots,
				Home:      roots[0],
				Commit:    true,
				Force:     f.force,
				Installer: d.installer(),
			})
			if err != nil {
				return fmt.Errorf("%s: %w", op, err)
			}
			return printReport(d, op, rep.Format())
		},
	}
	cmd.Flags().StringArrayVar(&f.roots, "root", nil, "absolute tree to restore (repeatable; defaults to /work; no $HOME default)")
	cmd.Flags().BoolVar(&f.force, "force", false, "discard live edits that no longer match the DriftHash install wrote")
	cmd.Flags().BoolVar(&f.yes, "yes", false, "skip the confirmation prompt; required when stdin is not a terminal")
	return cmd
}

// absRoots mirrors install's default exactly. If install displaced the mount
// root because --root was omitted, an uninstall that defaulted differently
// would leave every .pre-substrate orphaned and the rendered files live.
func absRoots(op string, roots []string) ([]string, error) {
	if len(roots) == 0 {
		return []string{cutover.DefaultMountRoot}, nil
	}
	out := append([]string(nil), roots...)
	for _, root := range out {
		if !filepath.IsAbs(root) {
			return nil, &UserError{
				What: op + ": --root is not absolute",
				Why:  "got " + root + "; roots are never guessed from $HOME",
				Next: "re-run with --root /work",
			}
		}
	}
	return out, nil
}

func printReport(d Deps, op, out string) error {
	if strings.TrimSpace(out) == "" {
		return &UserError{op + ": produced no plan", "nothing under --root matched", "check --root and --machine"}
	}
	_, err := fmt.Fprint(d.stdout(), out)
	return err
}

type cliRenderTarget struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	SHA256  string `json:"sha256"`
}

func fetchRenderTargets(ctx context.Context, api *apiClient, machine string) ([]cliRenderTarget, error) {
	q := url.Values{}
	if machine != "" {
		q.Set("machine", machine)
	}
	body, err := api.get(ctx, "/v1/render?"+q.Encode())
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Targets []cliRenderTarget `json:"targets"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, &UserError{
			What: api.op + ": unreadable render response",
			Why:  err.Error(),
			Next: "check that --server points at a Substrate control plane",
		}
	}
	return parsed.Targets, nil
}
