package adapter

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var gitSHAPattern = regexp.MustCompile(`^(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64})$`)

const maxSkillFileBytes = 1 << 20

var harnessSkillDirs = []string{
	".claude/skills",
	".codex/skills",
	".cursor/skills",
}

func linkSkills(ctx context.Context, db *DB, a *api, cfg Config, remotes []string) error {
	if cfg.SkillsRepo == "" {
		return nil
	}
	skills, err := a.getSkillsManifest(ctx, remotes)
	if err != nil {
		return err
	}
	mirror, err := ensureSkillsMirror(ctx, cfg)
	if err != nil {
		slog.Error("skills repo fetch failed; keeping last linked versions", "err", err)
		return nil
	}
	keep := make(map[string]struct{}, len(skills))
	for _, s := range skills {
		if err := materializeSkill(ctx, db, cfg, mirror, s); err != nil {
			slog.Error("skill materialize failed; keeping last linked version", "skill", s.Name, "err", err)
		}
		keep[s.Name] = struct{}{}
	}
	return pruneSkills(db, cfg, keep)
}

func ensureSkillsMirror(ctx context.Context, cfg Config) (string, error) {
	mirror := filepath.Join(cfg.Home, ".substrate", "skills.git")
	if err := Confine(cfg.Home, mirror); err != nil {
		return "", fmt.Errorf("adapter: skills mirror escapes home")
	}
	if _, err := os.Stat(filepath.Join(mirror, "HEAD")); err == nil {
		cmd := gitCommand(ctx, "--git-dir", mirror, "fetch", "--force", "--prune", "origin")
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("git fetch skills: %w: %s", err, bytes.TrimSpace(out))
		}
		return mirror, nil
	}
	if err := os.MkdirAll(filepath.Dir(mirror), 0o750); err != nil {
		return "", fmt.Errorf("adapter: skills mirror dir: %w", err)
	}
	cmd := gitCommand(ctx, "clone", "--bare", cfg.SkillsRepo, mirror)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git clone skills: %w: %s", err, bytes.TrimSpace(out))
	}
	return mirror, nil
}

func materializeSkill(ctx context.Context, db *DB, cfg Config, mirror string, s manifestSkill) error {
	if err := validateSkillRef(s); err != nil {
		return err
	}
	dest := filepath.Join(cfg.Home, ".agents", "skills", filepath.FromSlash(s.Name))
	if err := Confine(cfg.Home, dest); err != nil {
		return fmt.Errorf("adapter: skill %s escapes home", s.Name)
	}
	prev, has, err := db.getSkillLink(s.Name)
	if err != nil {
		return err
	}
	_, destErr := os.Stat(dest)
	if !has || prev.GitSHA != s.GitSHA || destErr != nil {
		if err := exportSkillTree(ctx, mirror, s.GitSHA, s.GitPath, dest); err != nil {
			return err
		}
	}
	links, err := refreshSkillSymlinks(cfg, s.Name, dest)
	if err != nil {
		return err
	}
	return db.upsertSkillLink(s.Name, s.GitSHA, strings.Join(links, "\n"))
}

func validateSkillRef(s manifestSkill) error {
	if s.Name == "" || strings.Contains(s.Name, "..") || filepath.IsAbs(s.Name) {
		return fmt.Errorf("adapter: refusing skill name %q", s.Name)
	}
	if s.GitPath == "" || strings.Contains(s.GitPath, "..") || filepath.IsAbs(s.GitPath) || strings.Contains(s.GitPath, "\x00") {
		return fmt.Errorf("adapter: refusing git_path %q", s.GitPath)
	}
	if !gitSHAPattern.MatchString(s.GitSHA) {
		return fmt.Errorf("adapter: refusing git_sha %q", s.GitSHA)
	}
	return nil
}

func exportSkillTree(ctx context.Context, gitDir, sha, gitPath, dest string) error {
	resolved, err := verifyCommit(ctx, gitDir, sha)
	if err != nil {
		return err
	}
	rel := strings.TrimPrefix(gitPath, "/")
	if err := disableArchiveSubst(gitDir); err != nil {
		return err
	}
	cmd := gitCommand(ctx, "--git-dir", gitDir, "archive", "--format=tar", resolved, "--", rel)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("git archive %s: %w", resolved, err)
	}
	parent := filepath.Dir(dest)
	if err := os.MkdirAll(parent, 0o750); err != nil {
		return fmt.Errorf("adapter: mkdir %s: %w", parent, err)
	}
	tmp, err := os.MkdirTemp(parent, ".skill-*")
	if err != nil {
		return fmt.Errorf("adapter: skill temp: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	prefix := filepath.ToSlash(rel) + "/"
	tr := tar.NewReader(bytes.NewReader(out))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("adapter: skill tar: %w", err)
		}
		switch hdr.Typeflag {
		case tar.TypeXHeader, tar.TypeXGlobalHeader, tar.TypeGNULongName, tar.TypeGNULongLink:
			continue
		}
		name := filepath.ToSlash(filepath.Clean(hdr.Name))
		relSlash := filepath.ToSlash(rel)
		if name == "." || name == relSlash {
			continue
		}
		if relSlash != "" && strings.HasPrefix(relSlash, name+"/") {
			continue
		}
		if !strings.HasPrefix(name, prefix) {
			return fmt.Errorf("adapter: refusing archive path %s", hdr.Name)
		}
		name = strings.TrimPrefix(name, prefix)
		if name == "" || name == "." {
			continue
		}
		if filepath.IsAbs(name) || strings.HasPrefix(name, "..") {
			return fmt.Errorf("adapter: refusing archive path %s", hdr.Name)
		}
		path := filepath.Join(tmp, name)
		if err := Confine(tmp, path); err != nil {
			return fmt.Errorf("adapter: archive path %s escapes dest", hdr.Name)
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(path, 0o750); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
				return err
			}
			f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644) //nolint:gosec // mode is a skill file under Home
			if err != nil {
				return err
			}
			n, err := io.Copy(f, io.LimitReader(tr, maxSkillFileBytes+1))
			if err != nil {
				_ = f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
			if n > maxSkillFileBytes {
				return fmt.Errorf("adapter: skill file %s exceeds %d bytes", hdr.Name, maxSkillFileBytes)
			}
		default:
			continue
		}
	}
	if err := os.RemoveAll(dest); err != nil {
		return fmt.Errorf("adapter: replace skill %s: %w", dest, err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		return fmt.Errorf("adapter: commit skill %s: %w", dest, err)
	}
	return nil
}

func refreshSkillSymlinks(cfg Config, name, dest string) ([]string, error) {
	abs, err := filepath.Abs(dest)
	if err != nil {
		return nil, err
	}
	var links []string
	for _, harness := range harnessSkillDirs {
		link := filepath.Join(cfg.Home, harness, filepath.FromSlash(name))
		if err := Confine(cfg.Home, link); err != nil {
			return nil, fmt.Errorf("adapter: skill symlink %s escapes home", link)
		}
		if err := os.MkdirAll(filepath.Dir(link), 0o750); err != nil {
			return nil, err
		}
		_ = os.Remove(link)
		if err := os.Symlink(abs, link); err != nil {
			return nil, fmt.Errorf("adapter: symlink %s: %w", link, err)
		}
		links = append(links, link)
	}
	return links, nil
}

func pruneSkills(db *DB, cfg Config, keep map[string]struct{}) error {
	rows, err := db.listSkillLinks()
	if err != nil {
		return err
	}
	for _, row := range rows {
		if _, ok := keep[row.Name]; ok {
			continue
		}
		dest := filepath.Join(cfg.Home, ".agents", "skills", filepath.FromSlash(row.Name))
		if err := Confine(cfg.Home, dest); err == nil {
			_ = os.RemoveAll(dest)
		}
		for _, p := range strings.Split(row.LinkedPaths, "\n") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if err := Confine(cfg.Home, p); err == nil {
				_ = os.Remove(p)
			}
		}
		if err := db.deleteSkillLink(row.Name); err != nil {
			return err
		}
	}
	return nil
}

func gitCommand(ctx context.Context, args ...string) *exec.Cmd {
	//nolint:gosec // argv is our literals plus operator-configured SkillsRepo / validated sha:path
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
	return cmd
}

func verifyCommit(ctx context.Context, gitDir, sha string) (string, error) {
	cmd := gitCommand(ctx, "--git-dir", gitDir, "rev-parse", "--verify", sha+"^{commit}")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse %s^{commit}: %w", sha, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func disableArchiveSubst(gitDir string) error {
	info := filepath.Join(gitDir, "info")
	if err := os.MkdirAll(info, 0o750); err != nil {
		return fmt.Errorf("adapter: git info dir: %w", err)
	}
	// info/attributes outranks tree .gitattributes, so export-subst cannot rewrite blobs.
	if err := os.WriteFile(filepath.Join(info, "attributes"), []byte("* -export-subst\n"), 0o600); err != nil {
		return fmt.Errorf("adapter: git attributes: %w", err)
	}
	return nil
}
