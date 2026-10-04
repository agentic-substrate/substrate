package authority

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func gitOutput(path string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", path}, args...)...)
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "GIT_") {
			cmd.Env = append(cmd.Env, value)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	output, err := cmd.Output()
	return strings.TrimSuffix(string(output), "\n"), err
}
func checkout(path string) (Binding, error) {
	root, err := gitOutput(path, "rev-parse", "--show-toplevel")
	if err != nil {
		return Binding{}, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return Binding{}, err
	}
	common, err := gitOutput(path, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return Binding{}, err
	}
	common, err = filepath.EvalSymlinks(common)
	if err != nil {
		return Binding{}, err
	}
	trees, err := gitOutput(path, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return Binding{}, err
	}
	recognized := false
	for _, field := range strings.Split(trees, "\x00") {
		if field == "worktree "+root {
			recognized = true
		}
	}
	if !recognized {
		return Binding{}, ErrDenied
	}
	info, err := os.Stat(common)
	if err != nil {
		return Binding{}, err
	}
	stat := info.Sys().(*syscall.Stat_t)
	return Binding{Checkout: root, CommonDir: common, Device: uint64(stat.Dev), Inode: stat.Ino}, nil
}
