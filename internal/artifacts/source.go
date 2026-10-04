package artifacts

import (
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"
)

func sourceGit(checkout string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", checkout, "--literal-pathspecs", "--no-replace-objects"}, args...)...)
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "GIT_") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	// An empty transport allowlist also blocks implicit promisor fetch helpers.
	cmd.Env = append(cmd.Env, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0", "GIT_ALLOW_PROTOCOL=")
	return cmd.Output()
}

func readSource(checkout string, src Source) (string, Source, error) {
	if !validText(src.Commit, src.Path, src.Blob) {
		return "", Source{}, ErrInvalidText
	}
	invalid := errors.New("source unavailable: use a full Git commit and a regular repository-relative file")
	if _, err := hex.DecodeString(src.Commit); err != nil || (len(src.Commit) != 40 && len(src.Commit) != 64) || strings.ToLower(src.Commit) != src.Commit || src.Path == "" || len(src.Path) > 4096 || strings.ContainsAny(src.Path, "\x00\r\n\\:") || path.IsAbs(src.Path) || path.Clean(src.Path) != src.Path || src.Path == "." || src.Path == ".." || strings.HasPrefix(src.Path, "../") {
		return "", Source{}, invalid
	}
	commit, err := sourceGit(checkout, "rev-parse", "--verify", src.Commit+"^{commit}")
	if err != nil || strings.TrimSpace(string(commit)) != src.Commit {
		return "", Source{}, invalid
	}
	entry, err := sourceGit(checkout, "ls-tree", "-z", src.Commit, "--", src.Path)
	if err != nil {
		return "", Source{}, invalid
	}
	records := strings.Split(string(entry), "\x00")
	if len(records) != 2 || records[1] != "" {
		return "", Source{}, invalid
	}
	metadata, returnedPath, ok := strings.Cut(records[0], "\t")
	if !ok || returnedPath != src.Path {
		return "", Source{}, invalid
	}
	fields := strings.Fields(metadata)
	if len(fields) != 3 || (fields[0] != "100644" && fields[0] != "100755") || fields[1] != "blob" {
		return "", Source{}, invalid
	}
	if src.Blob != "" && src.Blob != fields[2] {
		return "", Source{}, invalid
	}
	src.Blob = fields[2]
	size, err := sourceGit(checkout, "cat-file", "-s", src.Blob)
	if err != nil {
		return "", Source{}, invalid
	}
	// Check the blob size before allocating its contents.
	length, err := strconv.Atoi(strings.TrimSpace(string(size)))
	if err != nil || length < 1 || length > 1024*1024 {
		return "", Source{}, invalid
	}
	content, err := sourceGit(checkout, "cat-file", "blob", src.Blob)
	if err != nil || len(content) != length {
		return "", Source{}, invalid
	}
	if !utf8.Valid(content) {
		return "", Source{}, ErrInvalidText
	}
	return string(content), src, nil
}
