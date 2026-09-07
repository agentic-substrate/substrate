package cutover

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/agentic-substrate/substrate/internal/adapter"
	"github.com/agentic-substrate/substrate/internal/render"
)

const (
	memorixStore = ".memorix"
	journalRel   = ".substrate/cutover-journal.json"
)

// Cutover displaces existing harness files and stores to *.pre-substrate,
// writes rendered replacements, and installs the adapter unit. Dry-run
// (Commit=false) performs every read and decision, prints the plan, and
// writes nothing.
func Cutover(req Request) (*Report, error) {
	if err := validateRoots(req.Roots); err != nil {
		return nil, err
	}
	rep, err := planCutover(req)
	if err != nil {
		return nil, err
	}
	rep.DryRun = !req.Commit
	if !req.Commit {
		return rep, nil
	}
	if req.Installer == nil {
		return nil, fmt.Errorf("cutover: unit installer is required")
	}
	if err := applyCutover(rep, req); err != nil {
		return nil, err
	}
	return rep, nil
}

func planCutover(req Request) (*Report, error) {
	spec := unitSpec(req)
	rep := &Report{
		Unit:    &spec,
		Install: true,
	}
	for _, f := range req.Files {
		if err := classifyReplacement(req.Roots, f, rep); err != nil {
			return nil, err
		}
	}
	for _, root := range req.Roots {
		if err := classifyStore(root, memorixStore, rep); err != nil {
			return nil, err
		}
	}
	return rep, nil
}

func classifyReplacement(roots []string, f Replacement, rep *Report) error {
	if !filepath.IsAbs(f.Path) {
		return fmt.Errorf("cutover: replacement path must be absolute, got %q", f.Path)
	}
	if err := confineToRoots(roots, f.Path); err != nil {
		return err
	}
	backup := f.Path + BackupSuffix
	if _, err := os.Lstat(backup); err == nil {
		return fmt.Errorf("cutover: refusing to overwrite existing %s", backup)
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cutover: stat %s: %w", backup, err)
	}

	w := Write{
		Path:     f.Path,
		WouldSHA: sha256Hex([]byte(f.Content)),
		WouldDr:  render.DriftHash(f.Content),
		Mode:     0o600,
	}
	info, err := os.Lstat(f.Path)
	switch {
	case os.IsNotExist(err):
		rep.Created = append(rep.Created, f.Path)
	case err != nil:
		return fmt.Errorf("cutover: stat %s: %w", f.Path, err)
	default:
		if info.IsDir() {
			return fmt.Errorf("cutover: %s is a directory", f.Path)
		}
		sum, mode, hashErr := fileHash(f.Path)
		if hashErr != nil {
			return fmt.Errorf("cutover: read %s: %w", f.Path, hashErr)
		}
		w.CurrentSHA = sum
		w.CurrentDr = driftOf(f.Path)
		w.Mode = mode
		rep.Renames = append(rep.Renames, Rename{
			From: f.Path,
			To:   backup,
			Mode: mode,
			SHA:  sum,
		})
	}
	rep.Writes = append(rep.Writes, w)
	return nil
}

func classifyStore(root, name string, rep *Report) error {
	store := filepath.Join(root, name)
	info, err := os.Lstat(store)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cutover: stat %s: %w", store, err)
	}
	backup := store + BackupSuffix
	if _, err := os.Lstat(backup); err == nil {
		return fmt.Errorf("cutover: refusing to overwrite existing %s", backup)
	} else if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cutover: stat %s: %w", backup, err)
	}
	sha := ""
	if info.Mode().IsRegular() {
		sum, _, hashErr := fileHash(store)
		if hashErr != nil {
			return fmt.Errorf("cutover: read %s: %w", store, hashErr)
		}
		sha = sum
	}
	rep.Renames = append(rep.Renames, Rename{
		From: store,
		To:   backup,
		Mode: info.Mode(),
		SHA:  sha,
	})
	return nil
}

func confineToRoots(roots []string, path string) error {
	var escaped bool
	for _, root := range roots {
		err := adapter.Confine(root, path)
		if err == nil {
			return nil
		}
		if strings.Contains(err.Error(), "escapes") {
			escaped = true
		}
	}
	if escaped {
		return fmt.Errorf("cutover: path %s escapes -root", path)
	}
	return fmt.Errorf("cutover: path %s is not under any -root", path)
}

func applyCutover(rep *Report, req Request) error {
	write := atomicWrite
	if req.WriteFile != nil {
		write = req.WriteFile
	}
	if err := applyRenames(rep.Renames); err != nil {
		return errWithPlan(rep, err)
	}
	if err := writeJournal(req, journal{
		Created: rep.Created,
		Renames: rep.Renames,
		Pending: true,
	}); err != nil {
		return errWithPlan(rep, err)
	}
	contents := map[string]string{}
	for _, f := range req.Files {
		contents[f.Path] = f.Content
	}
	for _, w := range rep.Writes {
		body, ok := contents[w.Path]
		if !ok {
			return errWithPlan(rep, fmt.Errorf("cutover: missing content for %s", w.Path))
		}
		if err := write(w.Path, []byte(body), w.Mode); err != nil {
			return errWithPlan(rep, fmt.Errorf("cutover: write %s: %w (run adapter uninstall --restore to put files back)", w.Path, err))
		}
	}
	hashes := map[string]string{}
	for _, w := range rep.Writes {
		if body, ok := contents[w.Path]; ok {
			hashes[w.Path] = render.DriftHash(body)
		}
	}
	if err := writeJournal(req, journal{
		Created: rep.Created,
		Hashes:  hashes,
		Renames: rep.Renames,
		Pending: false,
	}); err != nil {
		return errWithPlan(rep, err)
	}
	spec := UnitSpec{}
	if rep.Unit != nil {
		spec = *rep.Unit
	}
	if err := req.Installer.Install(spec); err != nil {
		return errWithPlan(rep, err)
	}
	return nil
}

func errWithPlan(rep *Report, err error) error {
	if err == nil {
		return nil
	}
	if rep == nil {
		return err
	}
	plan := strings.TrimSpace(rep.Format())
	if plan == "" {
		return err
	}
	return fmt.Errorf("%w\n\nplan:\n%s", err, plan)
}

type journal struct {
	Created []string          `json:"created"`
	Hashes  map[string]string `json:"hashes,omitempty"`
	Renames []Rename          `json:"renames,omitempty"`
	Pending bool              `json:"pending,omitempty"`
}

func journalPath(home string) string {
	return filepath.Join(home, filepath.FromSlash(journalRel))
}

func writeJournal(req Request, j journal) error {
	home := req.Home
	if home == "" && len(req.Roots) > 0 {
		home = req.Roots[0]
	}
	if home == "" {
		return fmt.Errorf("cutover: journal requires Home")
	}
	path := journalPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("cutover: mkdir journal: %w", err)
	}
	raw, err := json.Marshal(j)
	if err != nil {
		return err
	}
	return atomicWrite(path, raw, 0o600)
}

func readJournal(home string) (*journal, error) {
	path := journalPath(home)
	raw, err := os.ReadFile(path) //nolint:gosec // journal is under an operator-supplied -root
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cutover: read journal %s: %w", path, err)
	}
	var j journal
	if err := json.Unmarshal(raw, &j); err != nil {
		return nil, fmt.Errorf("cutover: parse journal %s: %w", path, err)
	}
	return &j, nil
}

var (
	syncFile      = func(f *os.File) error { return f.Sync() }
	syncParentDir = fsyncDir
)

func fsyncDir(dir string) error {
	d, err := os.Open(dir) //nolint:gosec // dir is the dest parent under an operator-supplied -root
	if err != nil {
		return fmt.Errorf("cutover: open dir %s: %w", dir, err)
	}
	defer func() { _ = d.Close() }()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("cutover: sync dir %s: %w", dir, err)
	}
	return nil
}

func atomicWrite(path string, body []byte, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o600
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("cutover: mkdir %s: %w", dir, err)
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("cutover: temp file: %w", err)
	}
	tmp := f.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmp)
		}
	}()
	if _, err := f.Write(body); err != nil {
		_ = f.Close()
		return fmt.Errorf("cutover: write temp: %w", err)
	}
	if err := syncFile(f); err != nil {
		_ = f.Close()
		return fmt.Errorf("cutover: sync temp: %w", err)
	}
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return fmt.Errorf("cutover: chmod temp: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("cutover: close temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("cutover: rename temp: %w", err)
	}
	cleanup = false
	if err := syncParentDir(dir); err != nil {
		return err
	}
	return nil
}
