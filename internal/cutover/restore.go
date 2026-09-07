package cutover

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Restore puts *.pre-substrate paths back over the live paths they displaced
// and uninstalls the adapter unit. Dry-run (Commit=false) performs every
// read and classification, prints the plan, and writes nothing.
func Restore(req Request) (*Report, error) {
	if err := validateRoots(req.Roots); err != nil {
		return nil, err
	}
	rep, err := planRestore(req)
	if err != nil {
		return nil, err
	}
	rep.DryRun = !req.Commit
	if !req.Commit {
		return rep, nil
	}
	if err := applyRestore(rep, req.Installer); err != nil {
		return nil, err
	}
	return rep, nil
}

func planRestore(req Request) (*Report, error) {
	spec := unitSpec(req)
	rep := &Report{
		Unit:      &spec,
		Uninstall: true,
	}
	seenJournal := map[string]struct{}{}
	for _, root := range req.Roots {
		backups, err := findBackups(root)
		if err != nil {
			return nil, err
		}
		for _, b := range backups {
			if err := classifyRestore(b, rep); err != nil {
				return nil, err
			}
		}
		if err := loadJournal(root, rep, seenJournal); err != nil {
			return nil, err
		}
	}
	if req.Home != "" {
		if err := loadJournal(req.Home, rep, seenJournal); err != nil {
			return nil, err
		}
	}
	return rep, nil
}

func loadJournal(home string, rep *Report, seen map[string]struct{}) error {
	if _, ok := seen[home]; ok {
		return nil
	}
	seen[home] = struct{}{}
	j, err := readJournal(home)
	if err != nil {
		return err
	}
	if j != nil {
		rep.Created = append(rep.Created, j.Created...)
	}
	return nil
}

type backup struct {
	Path string
	Orig string
	Dir  bool
	Mode os.FileMode
	SHA  string
}

func findBackups(root string) ([]backup, error) {
	var out []backup
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		if !strings.HasSuffix(d.Name(), BackupSuffix) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return fmt.Errorf("cutover: stat %s: %w", path, err)
		}
		b := backup{
			Path: path,
			Orig: strings.TrimSuffix(path, BackupSuffix),
			Dir:  d.IsDir(),
			Mode: info.Mode(),
		}
		if d.IsDir() {
			out = append(out, b)
			return filepath.SkipDir
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("cutover: backup %s is not a regular file", path)
		}
		body, err := os.ReadFile(path) //nolint:gosec // path is under an operator-supplied -root
		if err != nil {
			return fmt.Errorf("cutover: read %s: %w", path, err)
		}
		b.SHA = sha256Hex(body)
		out = append(out, b)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func classifyRestore(b backup, rep *Report) error {
	live, err := os.Lstat(b.Orig)
	switch {
	case err == nil:
		if live.IsDir() != b.Dir {
			kind := "file"
			if b.Dir {
				kind = "directory"
			}
			liveKind := "file"
			if live.IsDir() {
				liveKind = "directory"
			}
			return fmt.Errorf("cutover: cannot restore %s (%s) over %s (%s)", b.Path, kind, b.Orig, liveKind)
		}
	case os.IsNotExist(err):
		// Fine: the live path was renamed away and nothing took its place.
	default:
		return fmt.Errorf("cutover: stat %s: %w", b.Orig, err)
	}

	w := Write{Path: b.Orig, Mode: b.Mode, WouldSHA: b.SHA}
	if err == nil && !live.IsDir() {
		sum, mode, hashErr := fileHash(b.Orig)
		if hashErr != nil {
			return fmt.Errorf("cutover: read %s: %w", b.Orig, hashErr)
		}
		w.CurrentSHA = sum
		w.Mode = mode
		w.CurrentDr = driftOf(b.Orig)
	}
	if !b.Dir {
		w.WouldDr = driftOf(b.Path)
	}
	rep.Writes = append(rep.Writes, w)
	rep.Renames = append(rep.Renames, Rename{
		From: b.Path,
		To:   b.Orig,
		Mode: b.Mode,
		SHA:  b.SHA,
	})
	return nil
}

func applyRestore(rep *Report, inst UnitInstaller) error {
	if err := applyRenames(rep.Renames); err != nil {
		return err
	}
	seen := map[string]struct{}{}
	for _, p := range rep.Created {
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cutover: remove generated %s: %w", p, err)
		}
	}
	if rep.Unit != nil && rep.Unit.Home != "" {
		jp := journalPath(rep.Unit.Home)
		if err := os.Remove(jp); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("cutover: remove journal: %w", err)
		}
		dir := filepath.Dir(jp)
		_ = os.Remove(dir) // only succeeds if empty; leftover adapter state stays
	}
	if inst == nil {
		return fmt.Errorf("cutover: unit installer is required")
	}
	spec := UnitSpec{}
	if rep.Unit != nil {
		spec = *rep.Unit
	}
	return inst.Uninstall(spec)
}
