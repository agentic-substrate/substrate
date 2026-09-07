// Package cutover displaces harness files and stores during import cutover
// and puts them back with `adapter uninstall --restore` (EDD §9, §15).
// Nothing in this flow deletes: displaced paths are renamed to
// *.pre-substrate, and restore renames them back (Gotcha 6).
package cutover

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/agentic-substrate/substrate/internal/render"
)

// BackupSuffix is appended to every displaced path. Never overwrite a path
// that already has this suffix — that is the one copy of an earlier cutover.
const BackupSuffix = ".pre-substrate"

// DefaultMountRoot is the CONT-4 checkout root written into the adapter unit
// so Phase 3 cwd slugs match later. Tests assert the unit contains this
// string; they must not exec against a real /work.
const DefaultMountRoot = "/work"

// Request is restore or cutover input. Roots must be absolute; there is no
// $HOME default. Tests pass t.TempDir(); the operator must pass -root.
type Request struct {
	Roots     []string
	Home      string
	Commit    bool
	Installer UnitInstaller
	// Files are rendered replacements for cutover. Restore ignores them.
	Files []Replacement
	// Server/Token/Machine/Binary feed the unit spec on cutover install.
	Server  string
	Token   string
	Machine string
	Binary  string
	// WriteFile writes one rendered replacement. Tests inject a failing
	// writer; nil uses atomicWrite. Production never sets this.
	WriteFile func(path string, body []byte, mode os.FileMode) error
	// Force allows restore to discard live edits that no longer match the
	// DriftHash cutover wrote.
	Force bool
}

// Replacement is one rendered file cutover would write.
type Replacement struct {
	Path    string
	Content string
}

// Report is the dry-run preview and the commit result. Dry-run and commit
// share Plan+Apply; Format is what the CLI prints.
type Report struct {
	DryRun    bool
	Renames   []Rename
	Writes    []Write
	Created   []string
	Unit      *UnitSpec
	Uninstall bool
	Install   bool
	// CreatedHashes is path → render.DriftHash for files cutover created.
	// Restore only removes a Created path when the live file still matches.
	CreatedHashes  map[string]string
	JournalPending bool
	Discard        []string
}

// Rename is one *.pre-substrate displacement or its inverse.
type Rename struct {
	From string
	To   string
	Mode os.FileMode
	SHA  string
}

// Write is one rendered-file replacement (cutover) or the live path restore
// would put back (current vs would hashes).
type Write struct {
	Path       string
	Mode       os.FileMode
	CurrentSHA string
	WouldSHA   string
	CurrentDr  string
	WouldDr    string
}

// WithMountRoot appends DefaultMountRoot so Discover and confineToRoots
// see /work/<project>/<repo> even when the operator only passed -root $HOME.
func WithMountRoot(roots []string) []string {
	out := append([]string(nil), roots...)
	for _, r := range roots {
		if r == DefaultMountRoot {
			return out
		}
	}
	return append(out, DefaultMountRoot)
}

func validateRoots(roots []string) error {
	if len(roots) == 0 {
		return fmt.Errorf("cutover: -root is required (refuses to guess $HOME)")
	}
	for _, root := range roots {
		if !filepath.IsAbs(root) {
			return fmt.Errorf("cutover: -root must be an absolute path, got %q (refuses to guess $HOME)", root)
		}
	}
	return nil
}

func unitSpec(req Request) UnitSpec {
	home := req.Home
	if home == "" && len(req.Roots) > 0 {
		home = req.Roots[0]
	}
	return UnitSpec{
		Binary:  req.Binary,
		Home:    home,
		Server:  req.Server,
		Token:   req.Token,
		Machine: req.Machine,
		Roots:   []string{DefaultMountRoot},
	}
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func fileHash(path string) (string, os.FileMode, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", 0, err
	}
	if !info.Mode().IsRegular() {
		return "", info.Mode(), nil
	}
	body, err := os.ReadFile(path) //nolint:gosec // path is under an operator-supplied -root
	if err != nil {
		return "", info.Mode(), err
	}
	return sha256Hex(body), info.Mode(), nil
}

func driftOf(path string) string {
	body, err := os.ReadFile(path) //nolint:gosec // path is under an operator-supplied -root
	if err != nil {
		return ""
	}
	return render.DriftHash(string(body))
}

// Format prints every planned rename, replacement, and unit change.
// Empty means nothing would change — dry-run tests require this to be
// non-empty when work exists.
func (r *Report) Format() string {
	if r == nil {
		return ""
	}
	var b strings.Builder
	if r.DryRun {
		b.WriteString("dry-run\n")
	}
	verb := "REPLACE"
	if r.Uninstall && !r.Install {
		verb = "RESTORE"
	}
	writes := append([]Write(nil), r.Writes...)
	sort.Slice(writes, func(i, j int) bool { return writes[i].Path < writes[j].Path })
	for _, w := range writes {
		fmt.Fprintf(&b, "%s %s\n", verb, w.Path)
		fmt.Fprintf(&b, "  current sha256: %s\n", dash(w.CurrentSHA))
		fmt.Fprintf(&b, "  would sha256: %s\n", dash(w.WouldSHA))
		if w.CurrentDr != "" || w.WouldDr != "" {
			fmt.Fprintf(&b, "  current drift: %s\n", dash(w.CurrentDr))
			fmt.Fprintf(&b, "  would drift: %s\n", dash(w.WouldDr))
		}
	}
	renames := append([]Rename(nil), r.Renames...)
	sort.Slice(renames, func(i, j int) bool { return renames[i].From < renames[j].From })
	for _, n := range renames {
		fmt.Fprintf(&b, "RENAME %s -> %s\n", n.From, n.To)
		if n.SHA != "" {
			fmt.Fprintf(&b, "  sha256: %s\n", n.SHA)
		}
	}
	if r.Uninstall {
		b.WriteString("UNINSTALL adapter unit\n")
	}
	if r.Install && r.Unit != nil {
		fmt.Fprintf(&b, "INSTALL adapter unit (roots=%s)\n", strings.Join(r.Unit.Roots, ","))
	}
	if r.Uninstall && len(r.Discard) > 0 {
		b.WriteString("DISCARD live edits\n")
		disc := append([]string(nil), r.Discard...)
		sort.Strings(disc)
		for _, p := range disc {
			fmt.Fprintf(&b, "  %s\n", p)
		}
	}
	created := append([]string(nil), r.Created...)
	sort.Strings(created)
	for _, p := range created {
		if r.Uninstall && !r.Install {
			fmt.Fprintf(&b, "REMOVE generated %s\n", p)
		} else {
			fmt.Fprintf(&b, "CREATE %s\n", p)
		}
	}
	return b.String()
}

func dash(s string) string {
	if s == "" {
		return "(absent)"
	}
	return s
}

func applyRenames(renames []Rename) error {
	var done []Rename
	for _, n := range renames {
		if strings.HasSuffix(n.To, BackupSuffix) {
			if _, err := os.Lstat(n.To); err == nil {
				if rbErr := rollbackRenames(done); rbErr != nil {
					return fmt.Errorf("cutover: refusing to overwrite existing %s (rollback: %w)", n.To, rbErr)
				}
				return fmt.Errorf("cutover: refusing to overwrite existing %s", n.To)
			} else if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("cutover: stat %s: %w", n.To, err)
			}
		}
		if err := os.Rename(n.From, n.To); err != nil {
			if rbErr := rollbackRenames(done); rbErr != nil {
				return fmt.Errorf("cutover: rename %s -> %s: %w (rollback: %w)", n.From, n.To, err, rbErr)
			}
			return fmt.Errorf("cutover: rename %s -> %s: %w", n.From, n.To, err)
		}
		done = append(done, n)
	}
	return nil
}

func rollbackRenames(done []Rename) error {
	for i := len(done) - 1; i >= 0; i-- {
		n := done[i]
		if err := os.Rename(n.To, n.From); err != nil {
			return fmt.Errorf("cutover: rollback %s -> %s: %w", n.To, n.From, err)
		}
	}
	return nil
}
