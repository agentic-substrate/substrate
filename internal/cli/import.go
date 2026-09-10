package cli

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/agentic-substrate/substrate/internal/importer"
	"github.com/spf13/cobra"
)

func newImportCmd(d Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Inventory, plan, and apply an import of existing harness files",
	}
	cmd.AddCommand(newImportScanCmd(d), newImportPlanCmd(d), newImportApplyCmd(d))
	return cmd
}

func newImportScanCmd(d Deps) *cobra.Command {
	var f struct {
		roots       []string
		hostname    string
		out         string
		memorixJSON string
		exclude     []string
	}
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Inventory harness files under one or more roots into inventory.json",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			const op = "import scan"
			if len(f.roots) == 0 {
				return &UserError{op + ": no --root", "scan refuses to guess $HOME", "re-run with --root /work"}
			}
			if f.hostname == "" {
				return &UserError{op + ": no --hostname", "inventoried files are tagged with the machine they came from", "re-run with --hostname $(hostname)"}
			}
			if err := requireAbsOut(op, f.out); err != nil {
				return err
			}
			if f.memorixJSON != "" && !filepath.IsAbs(f.memorixJSON) {
				return &UserError{op + ": --memorix-json is not absolute", "got " + f.memorixJSON, "pass the full path to the exported JSON"}
			}
			outPath := filepath.Clean(f.out)
			for _, root := range f.roots {
				if pathInside(filepath.Clean(root), outPath) {
					return &UserError{
						What: op + ": --out is inside --root",
						Why:  outPath + " is under " + root + ", so the scan would inventory its own output",
						Next: "write the inventory outside every --root",
					}
				}
			}
			inv, err := importer.Scan(importer.Request{
				Roots:       f.roots,
				Hostname:    f.hostname,
				MemorixJSON: f.memorixJSON,
				Exclude:     f.exclude,
			})
			if err != nil {
				return fmt.Errorf("%s: %w", op, err)
			}
			log := slog.New(slog.NewJSONHandler(d.stderr(), nil))
			for _, s := range inv.Skipped {
				log.Info("skipped source", "source", s.Source, "reason", s.Reason)
			}
			if err := writeJSON(f.out, inv); err != nil {
				return err
			}
			_, err = fmt.Fprintln(d.stdout(), f.out)
			return err
		},
	}
	cmd.Flags().StringArrayVar(&f.roots, "root", nil, "absolute search root (repeatable; required; no $HOME default)")
	cmd.Flags().StringVar(&f.hostname, "hostname", "", "machine name tagged on inventoried files")
	cmd.Flags().StringVar(&f.out, "out", "", "absolute path to write inventory.json")
	cmd.Flags().StringVar(&f.memorixJSON, "memorix-json", "", "absolute path to a pre-exported memorix JSON file")
	cmd.Flags().StringArrayVar(&f.exclude, "exclude", nil, "glob matched against a file's path relative to its root; ** spans separators (repeatable)")
	return cmd
}

func newImportPlanCmd(d Deps) *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "plan inventory.json...",
		Short: "Merge inventories into a reviewable plan.json",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			const op = "import plan"
			if err := requireAbsOut(op, out); err != nil {
				return err
			}
			invs := make([]importer.Inventory, 0, len(args))
			for _, p := range args {
				inv, err := readInventory(op, p)
				if err != nil {
					return err
				}
				invs = append(invs, inv)
			}
			outPath := filepath.Clean(out)
			for _, inv := range invs {
				for _, file := range inv.Files {
					if file.Path != "" && filepath.Clean(file.Path) == outPath {
						return &UserError{
							What: op + ": --out is an inventoried path",
							Why:  outPath + " appears in the inventory, so the plan would overwrite a source file",
							Next: "write the plan somewhere no inventory names",
						}
					}
				}
			}
			plan, err := importer.BuildPlan(invs, nil)
			if err != nil {
				return fmt.Errorf("%s: %w", op, err)
			}
			if err := writeJSON(out, plan); err != nil {
				return err
			}
			_, err = fmt.Fprintln(d.stdout(), out)
			return err
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "absolute path to write plan.json")
	return cmd
}

func newImportApplyCmd(d Deps) *cobra.Command {
	var f struct {
		machine   string
		trusted   string
		server    string
		token     string
		scope     string
		inventory []string
		yes       bool
	}
	cmd := &cobra.Command{
		Use:   "apply plan.json",
		Short: "Write a plan to the control plane (this commits)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			const op = "import apply"
			if f.machine == "" {
				return &UserError{op + ": no --machine", "rows are attributed to the machine they came from", "re-run with --machine $(hostname)"}
			}
			if f.trusted == "" {
				return &UserError{op + ": no --trusted", "only the most-trusted machine may create active rows", "re-run with --trusted <hostname>"}
			}
			api, err := newAPIClient(d, op, f.server, f.token)
			if err != nil {
				return err
			}
			target, err := resolveTarget(d, op, f.scope, scopeFlagsFrom(cmd))
			if err != nil {
				return err
			}
			plan, err := readPlan(op, args[0])
			if err != nil {
				return err
			}
			// The plan is an operator artifact; the inventory is the scan's own
			// product. Apply admits a block only because the inventory contains
			// it (#95), so --inventory is required: without it there is nothing
			// to check the plan against and the apply is refused server-side
			// anyway.
			if len(f.inventory) == 0 {
				return &UserError{
					What: op + ": no --inventory",
					Why:  "apply admits only blocks the scan recorded, and the plan alone cannot prove what the scan saw",
					Next: "re-run with --inventory for every inventory.json that import plan consumed",
				}
			}
			invs := make([]importer.Inventory, 0, len(f.inventory))
			for _, p := range f.inventory {
				inv, err := readInventory(op, p)
				if err != nil {
					return err
				}
				invs = append(invs, inv)
			}
			witness, err := importer.WitnessInventories(invs)
			if err != nil {
				return fmt.Errorf("%s: %w", op, err)
			}
			// Refuse locally too, so the operator sees the offending block
			// named beside the files it came from rather than a bare 403.
			if err := importer.CheckPlanWitness(plan, witness); err != nil {
				return &UserError{
					What: op + ": the plan does not match the inventory",
					Why:  err.Error(),
					Next: "regenerate the plan from these inventories with substrate import plan, and treat an unexplained block as a planted one",
				}
			}
			// Echo before the prompt, not after: the scope and the source
			// that won are the only thing the operator can review, and
			// GET /v1/import never discloses the chain afterwards.
			if _, err := fmt.Fprintln(d.stdout(), target.String()); err != nil {
				return err
			}
			if err := confirm(d, op, f.yes); err != nil {
				return err
			}
			payload := map[string]any{
				"machine":         f.machine,
				"trusted_machine": f.trusted,
				// The wire protocol keeps both fields; only the CLI surface
				// lost them. The server re-derives from this pair.
				"dry_run":           false,
				"commit":            true,
				"plan":              plan,
				"inventory_witness": witness,
			}
			if target.Scope != "" {
				payload["scope"] = target.Scope
			} else {
				payload["repo"] = target.Repo
			}
			body, err := json.Marshal(payload)
			if err != nil {
				return fmt.Errorf("%s: %w", op, err)
			}
			respBody, err := api.post(cmd.Context(), "/v1/import", body)
			if err != nil {
				return err
			}
			var parsed importer.ApplyResult
			if err := json.Unmarshal(respBody, &parsed); err == nil {
				if formatted := parsed.Format(); strings.TrimSpace(formatted) != "" {
					_, err = fmt.Fprint(d.stdout(), formatted)
					return err
				}
			}
			_, err = fmt.Fprint(d.stdout(), string(respBody))
			return err
		},
	}
	cmd.Flags().StringVar(&f.machine, "machine", "", "hostname this apply is for")
	cmd.Flags().StringVar(&f.trusted, "trusted", "", "most-trusted hostname; only it may create active rows")
	cmd.Flags().StringVar(&f.server, "server", "", "control plane base URL (default $SUBSTRATE_URL)")
	cmd.Flags().StringVar(&f.token, "token", "", "bearer token (default $SUBSTRATE_TOKEN)")
	cmd.Flags().StringVar(&f.scope, "scope", "", "scope path for imported rows; without it, the context file then the git remote decide")
	cmd.Flags().StringArrayVar(&f.inventory, "inventory", nil, "inventory.json the plan was built from (repeatable; required; apply admits only blocks it contains)")
	cmd.Flags().BoolVar(&f.yes, "yes", false, "skip the confirmation prompt; required when stdin is not a terminal")
	return cmd
}

func requireAbsOut(op, out string) error {
	if out == "" {
		return &UserError{op + ": no --out", "the artifact needs a destination", "re-run with --out /tmp/inventory.json"}
	}
	if !filepath.IsAbs(out) {
		return &UserError{op + ": --out is not absolute", "got " + out, "pass an absolute path"}
	}
	return nil
}

func readInventory(op, path string) (importer.Inventory, error) {
	var inv importer.Inventory
	raw, err := os.ReadFile(path) //nolint:gosec // operator-supplied inventory.json path
	if err != nil {
		return inv, &UserError{op + ": cannot read " + path, err.Error(), "check the path printed by import scan"}
	}
	if err := json.Unmarshal(raw, &inv); err != nil {
		return inv, &UserError{op + ": " + path + " is not an inventory", err.Error(), "regenerate it with substrate import scan"}
	}
	return inv, nil
}

func readPlan(op, path string) (importer.Plan, error) {
	var plan importer.Plan
	raw, err := os.ReadFile(path) //nolint:gosec // operator-supplied plan.json path
	if err != nil {
		return plan, &UserError{op + ": cannot read " + path, err.Error(), "check the path printed by import plan"}
	}
	if err := json.Unmarshal(raw, &plan); err != nil {
		return plan, &UserError{op + ": " + path + " is not a plan", err.Error(), "regenerate it with substrate import plan"}
	}
	return plan, nil
}

// pathInside reports whether child is root or a path under it. Rel-based so
// /home/u-other is not treated as inside /home/u.
func pathInside(root, child string) bool {
	rel, err := filepath.Rel(root, child)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// writeJSON writes v atomically at 0600: an artifact half-written over a
// previous one is worse than no artifact.
func writeJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	f, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	tmp := f.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmp)
		}
	}()
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if _, err := f.Write(raw); err != nil {
		_ = f.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	cleanup = false
	return nil
}
