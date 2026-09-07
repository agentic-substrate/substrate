package cutover

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/agentic-substrate/substrate/internal/adapter"
	"github.com/agentic-substrate/substrate/internal/render"
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
	if err := checkGeneratedRemovals(rep); err != nil {
		return nil, err
	}
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
	var journaled []string
	for _, root := range req.Roots {
		j, err := loadJournal(root, rep, seenJournal)
		if err != nil {
			return nil, err
		}
		journaled = append(journaled, journalLivePaths(j)...)
	}
	if req.Home != "" {
		j, err := loadJournal(req.Home, rep, seenJournal)
		if err != nil {
			return nil, err
		}
		journaled = append(journaled, journalLivePaths(j)...)
	}
	lives, err := plannedLivePaths(req)
	if err != nil {
		return nil, err
	}
	lives = uniquePaths(append(journaled, lives...))
	for _, live := range lives {
		b, err := backupIfPresent(live)
		if err != nil {
			// An unreadable planned path must not abort the rest of restore
			// the way import scan keeps walking past one permission error.
			continue
		}
		if b == nil {
			continue
		}
		if err := classifyRestore(*b, rep); err != nil {
			return nil, err
		}
	}
	return rep, nil
}

func plannedLivePaths(req Request) ([]string, error) {
	home := req.Home
	if home == "" && len(req.Roots) > 0 {
		home = req.Roots[0]
	}
	checkouts, err := adapter.Discover(WithMountRoot(req.Roots))
	if err != nil {
		return nil, err
	}
	var checkoutPaths []string
	for _, c := range checkouts {
		checkoutPaths = append(checkoutPaths, c.Path)
	}
	seen := map[string]struct{}{}
	var lives []string
	add := func(p string) {
		if p == "" {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		lives = append(lives, p)
	}
	for _, spec := range render.Specs() {
		for _, p := range spec.Paths {
			dests, destErr := Destinations(home, checkoutPaths, p)
			if destErr != nil {
				continue
			}
			for _, d := range dests {
				add(d)
			}
		}
	}
	for _, root := range req.Roots {
		add(filepath.Join(root, memorixStore))
	}
	if home != "" {
		add(systemdUnitPath(home))
		add(launchdPlistPath(home))
	}
	return lives, nil
}

func loadJournal(home string, rep *Report, seen map[string]struct{}) (*journal, error) {
	if _, ok := seen[home]; ok {
		return nil, nil
	}
	seen[home] = struct{}{}
	j, err := readJournal(home)
	if err != nil {
		return nil, err
	}
	if j != nil {
		rep.Created = append(rep.Created, j.Created...)
		if j.Pending {
			rep.JournalPending = true
		}
		if j.Hashes != nil {
			if rep.CreatedHashes == nil {
				rep.CreatedHashes = map[string]string{}
			}
			for p, h := range j.Hashes {
				rep.CreatedHashes[p] = h
			}
		}
	}
	return j, nil
}

func journalLivePaths(j *journal) []string {
	if j == nil {
		return nil
	}
	var out []string
	for _, n := range j.Renames {
		if n.From != "" {
			out = append(out, n.From)
		}
	}
	return out
}

func uniquePaths(in []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, p := range in {
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

type backup struct {
	Path string
	Orig string
	Dir  bool
	Mode os.FileMode
	SHA  string
}

func backupIfPresent(live string) (*backup, error) {
	path := live + BackupSuffix
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	b := backup{
		Path: path,
		Orig: live,
		Dir:  info.IsDir(),
		Mode: info.Mode(),
	}
	if info.IsDir() {
		return &b, nil
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("cutover: backup %s is not a regular file", path)
	}
	body, err := os.ReadFile(path) //nolint:gosec // path is a planned harness/store/unit backup under -root
	if err != nil {
		return nil, err
	}
	b.SHA = sha256Hex(body)
	return &b, nil
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
		if err := removeGenerated(p, rep); err != nil {
			return err
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

func checkGeneratedRemovals(rep *Report) error {
	if rep == nil || rep.JournalPending {
		return nil
	}
	seen := map[string]struct{}{}
	for _, p := range rep.Created {
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		if err := generatedRemovalConflict(p, rep); err != nil {
			return err
		}
	}
	return nil
}

func generatedRemovalConflict(p string, rep *Report) error {
	want, ok := rep.CreatedHashes[p]
	if !ok {
		return nil
	}
	body, err := os.ReadFile(p) //nolint:gosec // path is a journaled Created file under -root
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cutover: read generated %s: %w", p, err)
	}
	if render.DriftHash(string(body)) != want {
		return fmt.Errorf("cutover: %s no longer matches the written DriftHash (operator edits); refusing to remove", p)
	}
	return nil
}

func removeGenerated(p string, rep *Report) error {
	if rep.JournalPending {
		return nil
	}
	if err := generatedRemovalConflict(p, rep); err != nil {
		return err
	}
	if _, ok := rep.CreatedHashes[p]; !ok {
		return nil
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cutover: remove generated %s: %w", p, err)
	}
	return nil
}
