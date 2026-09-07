package adapter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentic-substrate/substrate/internal/instruction"
	"github.com/agentic-substrate/substrate/internal/render"
)

func TestRenderCallTimesOut(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		<-block
	}))
	t.Cleanup(func() {
		close(block)
		srv.Close()
	})

	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	cfg := testConfig(home, state, srv.URL)
	cfg.HTTPTimeout = 50 * time.Millisecond

	done := make(chan error, 1)
	go func() {
		_, err := Sync(context.Background(), db, cfg)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Sync succeeded against a hung GET /v1/render")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("GET /v1/render did not time out")
	}
}

func TestReviewCallTimesOut(t *testing.T) {
	home := t.TempDir()
	dest := filepath.Join(home, ".claude", "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		t.Fatal(err)
	}
	preexisting := "keep-this-untracked-file\n"
	if err := os.WriteFile(dest, []byte(preexisting), 0o600); err != nil {
		t.Fatal(err)
	}
	original := claudeAt(t, "2026-08-20T00:00:00Z")
	block := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/render", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"targets": []target{homeTarget(original)},
		})
	})
	mux.HandleFunc("POST /v1/review", func(_ http.ResponseWriter, _ *http.Request) {
		<-block
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(func() {
		select {
		case <-block:
		default:
			close(block)
		}
		srv.Close()
	})

	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	cfg := testConfig(home, state, srv.URL)
	cfg.HTTPTimeout = 50 * time.Millisecond

	done := make(chan error, 1)
	go func() {
		_, err := Sync(context.Background(), db, cfg)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Sync succeeded against a hung POST /v1/review")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("POST /v1/review did not time out")
	}
	if got := readFile(t, dest); got != preexisting {
		t.Fatalf("untracked file was overwritten while review hung: %q", got)
	}
}

func TestDefaultRenderIntervalIsFiveMinutes(t *testing.T) {
	if DefaultInterval != 5*time.Minute {
		t.Fatalf("DefaultInterval = %s, want 5m (SYNC-1, EDD §7.2)", DefaultInterval)
	}
}

func TestAtomicWriteFsyncsParentDir(t *testing.T) {
	orig := syncParentDir
	t.Cleanup(func() { syncParentDir = orig })
	var got string
	syncParentDir = func(dir string) error {
		got = dir
		return orig(dir)
	}
	dir := t.TempDir()
	dest := filepath.Join(dir, "CLAUDE.md")
	if err := AtomicWrite(dest, []byte("x\n")); err != nil {
		t.Fatalf("AtomicWrite: %v", err)
	}
	if got != dir {
		t.Fatalf("parent dir fsync path = %q, want dest dir %q", got, dir)
	}
}

func TestAtomicWriteSetsNewFileModeNotCreateTempDefault(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "CLAUDE.md")
	if err := AtomicWrite(dest, []byte("new content\n")); err != nil {
		t.Fatalf("AtomicWrite: %v", err)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Fatalf("mode = %04o, want 0644 (CreateTemp's 0600 must not become the dest mode)", perm)
	}
}

func TestAtomicWritePreservesExistingFileMode(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(dest, []byte("old\n"), 0o640); err != nil { //nolint:gosec // the test asserts this mode is preserved across rename
		t.Fatal(err)
	}
	if err := AtomicWrite(dest, []byte("new content\n")); err != nil {
		t.Fatalf("AtomicWrite: %v", err)
	}
	info, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o640 {
		t.Fatalf("mode = %04o, want 0640 preserved from the previous file", perm)
	}
}

func TestAtomicWriteReplacesFileAndLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	tmpdir := t.TempDir()
	if err := os.Chmod(tmpdir, 0o500); err != nil { //nolint:gosec // TMPDIR must not be writable; dest-dir temp must still succeed
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(tmpdir, 0o750) }) //nolint:gosec // restore perms so TempDir cleanup can run
	t.Setenv("TMPDIR", tmpdir)

	dest := filepath.Join(dir, "CLAUDE.md")
	old := []byte("old content\n")
	next := []byte("new content that is longer than old\n")
	if err := os.WriteFile(dest, old, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWrite(dest, next); err != nil {
		t.Fatalf("AtomicWrite: %v (temp must live in the dest dir, not os.TempDir)", err)
	}
	got, err := os.ReadFile(dest) //nolint:gosec // dest is under t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, next) && !bytes.Equal(got, old) {
		t.Fatalf("partial write survived: %q", got)
	}
	if !bytes.Equal(got, next) {
		t.Fatalf("got %q, want the new content", got)
	}
	assertNoTemp(t, dir)
}

func TestInterruptedWriteLeavesOldOrNewNeverPartial(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "CLAUDE.md")
	old := []byte("old-bytes-that-must-remain\n")
	next := []byte("replacement-that-must-not-be-partial\n")
	if err := AtomicWrite(dest, old); err != nil {
		t.Fatalf("setup AtomicWrite: %v", err)
	}
	if err := os.Chmod(dir, 0o555); err != nil { //nolint:gosec // the test needs the next write to fail
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o750) }) //nolint:gosec // restore perms so TempDir cleanup can run

	err := AtomicWrite(dest, next)
	if err == nil {
		t.Fatal("AtomicWrite succeeded in a read-only directory")
	}
	if !strings.Contains(err.Error(), "temp file") {
		t.Fatalf("error = %v; want temp-file creation to fail in a read-only dest dir. A temp in os.TempDir() would fail at rename instead", err)
	}

	if chmodErr := os.Chmod(dir, 0o750); chmodErr != nil { //nolint:gosec // restore directory perms after the failed write
		t.Fatal(chmodErr)
	}
	got, readErr := os.ReadFile(dest) //nolint:gosec // dest is under t.TempDir
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(got, old) && !bytes.Equal(got, next) {
		t.Fatalf("partial content %q; want old or new", got)
	}
	if !bytes.Equal(got, old) {
		t.Fatalf("interrupted write changed the dest; got %q", got)
	}
	assertNoTemp(t, dir)
}

func TestFooterOnlyChangeIsNotDrift(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)

	t1 := claudeAt(t, "2026-01-01T00:00:00Z")
	t2 := claudeAt(t, "2026-01-02T00:00:00Z")
	if t1.hash == t2.hash && t1.body == t2.body {
		t.Fatal("fixture collapsed: both renders are identical")
	}
	full1 := sha256.Sum256([]byte(t1.body))
	full2 := sha256.Sum256([]byte(t2.body))
	if hex.EncodeToString(full1[:]) == hex.EncodeToString(full2[:]) {
		t.Fatal("fixture is not load-bearing: whole-file hashes match, so a footer-blind test cannot fail")
	}
	if t1.hash != t2.hash {
		t.Fatal("DriftHash diverged on generated-at; the adapter would file noise every cycle")
	}

	srv := newFake(t)
	srv.setTargets(homeTarget(t1))
	cfg := testConfig(home, state, srv.URL)

	if _, err := Sync(t.Context(), db, cfg); err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	srv.setTargets(homeTarget(t2))
	if _, err := Sync(t.Context(), db, cfg); err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if n := srv.reviewCount(); n != 0 {
		t.Fatalf("footer-only change posted %d drift_proposal items; want 0", n)
	}
	got := readHomeClaude(t, home)
	if render.DriftHash(got) != t2.hash {
		t.Fatal("managed file drift-hash does not match the server after a footer-only refresh")
	}
}

func TestHandEditPostsDriftThenRestores(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	original := claudeAt(t, "2026-03-01T00:00:00Z")

	dest := filepath.Join(home, ".claude", "CLAUDE.md")
	var order []string
	srv := newFake(t)
	srv.setTargets(homeTarget(original))
	srv.reviewHook = func() {
		order = append(order, "review")
		got, err := os.ReadFile(dest) //nolint:gosec // dest is under t.TempDir
		if err != nil {
			t.Errorf("read during review: %v", err)
			return
		}
		if !strings.Contains(string(got), "hand-edit-marker") {
			t.Error("file was overwritten before the drift_proposal was posted")
		}
	}
	cfg := testConfig(home, state, srv.URL)

	if _, err := Sync(t.Context(), db, cfg); err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	edited := append([]byte(readFile(t, dest)), []byte("hand-edit-marker\n")...)
	if err := os.WriteFile(dest, edited, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Sync(t.Context(), db, cfg); err != nil {
		t.Fatalf("drift Sync: %v", err)
	}
	order = append(order, "restored")

	if len(order) < 2 || order[0] != "review" || order[1] != "restored" {
		t.Fatalf("order = %v, want review then restored", order)
	}
	reviews := srv.reviews()
	if len(reviews) != 1 {
		t.Fatalf("got %d reviews, want 1", len(reviews))
	}
	if reviews[0].Kind != "drift_proposal" {
		t.Fatalf("kind = %q, want drift_proposal", reviews[0].Kind)
	}
	diff := reviews[0].Diff
	if !strings.Contains(diff, "---") || !strings.Contains(diff, "+++") {
		t.Fatalf("payload.diff is not a unified diff: %q", diff)
	}
	if !strings.Contains(diff, "hand-edit-marker") {
		t.Fatalf("unified diff does not contain the local edit: %q", diff)
	}
	got := readHomeClaude(t, home)
	if strings.Contains(got, "hand-edit-marker") {
		t.Fatal("hand edit was not restored")
	}
	if got != original.body {
		t.Fatalf("restored file does not match the server")
	}
	if strings.Count(got, "<!-- generated — do not edit -->") != 1 {
		t.Fatal("adapter added a second generated header")
	}
}

func TestUntrackedExistingFilePostsDriftBeforeOverwrite(t *testing.T) {
	home := t.TempDir()
	dest := filepath.Join(home, ".claude", "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		t.Fatal(err)
	}
	preexisting := "operator-owned CLAUDE.md\n"
	if err := os.WriteFile(dest, []byte(preexisting), 0o600); err != nil {
		t.Fatal(err)
	}

	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	original := claudeAt(t, "2026-07-01T00:00:00Z")

	var order []string
	srv := newFake(t)
	srv.setTargets(homeTarget(original))
	srv.reviewHook = func() {
		order = append(order, "review")
		got, err := os.ReadFile(dest) //nolint:gosec // dest is under t.TempDir
		if err != nil {
			t.Errorf("read during review: %v", err)
			return
		}
		if string(got) != preexisting {
			t.Error("file was overwritten before the drift_proposal was posted")
		}
	}
	cfg := testConfig(home, state, srv.URL)

	if _, err := Sync(t.Context(), db, cfg); err != nil {
		t.Fatalf("Sync: %v", err)
	}

	reviews := srv.reviews()
	if len(reviews) != 1 {
		t.Fatalf("got %d reviews, want 1 (missing managed_file row is drift, not a blank disk)", len(reviews))
	}
	if reviews[0].Kind != "drift_proposal" {
		t.Fatalf("kind = %q, want drift_proposal", reviews[0].Kind)
	}
	if !strings.Contains(reviews[0].Diff, "operator-owned CLAUDE.md") {
		t.Fatalf("unified diff does not contain the preexisting body: %q", reviews[0].Diff)
	}
	if len(order) == 0 || order[0] != "review" {
		t.Fatalf("order = %v, want review before restore", order)
	}
	got := readHomeClaude(t, home)
	if got != original.body {
		t.Fatal("preexisting file was not restored after a successful drift_proposal")
	}
}

func TestUntrackedExistingFileNotOverwrittenWhenReviewFails(t *testing.T) {
	home := t.TempDir()
	dest := filepath.Join(home, ".claude", "CLAUDE.md")
	if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
		t.Fatal(err)
	}
	preexisting := "keep-this-untracked-file\n"
	if err := os.WriteFile(dest, []byte(preexisting), 0o600); err != nil {
		t.Fatal(err)
	}

	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	srv := newFake(t)
	srv.reviewStatus = http.StatusInternalServerError
	srv.setTargets(homeTarget(claudeAt(t, "2026-07-02T00:00:00Z")))
	cfg := testConfig(home, state, srv.URL)

	if _, err := Sync(t.Context(), db, cfg); err == nil {
		t.Fatal("Sync succeeded when POST /v1/review failed")
	}
	got := readFile(t, dest)
	if got != preexisting {
		t.Fatalf("untracked file was overwritten despite a failed drift_proposal: %q", got)
	}
}

func TestServerChangeReachesManagedFiles(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	repo := filepath.Join(root, "proj", "repo")
	initRepo(t, repo, "https://github.com/acme/known.git")

	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)

	before := pairAt(t, "2026-04-01T00:00:00Z", "3.12")
	after := pairAt(t, "2026-04-02T00:00:00Z", "3.13")

	srv := newFake(t)
	srv.bound["github.com/acme/known"] = true
	srv.setTargets(before.targets...)
	cfg := testConfig(home, state, srv.URL)
	cfg.Roots = []string{root}

	if _, err := Sync(t.Context(), db, cfg); err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	srv.setTargets(after.targets...)
	if _, err := Sync(t.Context(), db, cfg); err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	if n := srv.reviewCount(); n != 0 {
		t.Fatalf("authoritative server change posted %d reviews (SYNC-6 is server-wins, not merge)", n)
	}
	if got := readHomeClaude(t, home); got != after.claude.body {
		t.Fatal("home CLAUDE.md did not pick up the server change")
	}
	if got := readFile(t, filepath.Join(repo, "AGENTS.md")); got != after.agents.body {
		t.Fatal("repo AGENTS.md did not pick up the server change")
	}
}

func TestRelativeTargetsSkipUnknownRemoteCheckouts(t *testing.T) {
	root := t.TempDir()
	known := filepath.Join(root, "proj", "known")
	unknown := filepath.Join(root, "proj", "unknown")
	initRepo(t, known, "https://github.com/acme/known.git")
	initRepo(t, unknown, "https://github.com/acme/mystery.git")

	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	pair := pairAt(t, "2026-09-01T00:00:00Z", "3.12")
	srv := newFake(t)
	srv.bound["github.com/acme/known"] = true
	srv.setTargets(pair.targets...)
	cfg := testConfig(home, state, srv.URL)
	cfg.Roots = []string{root}

	res, err := Sync(t.Context(), db, cfg)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if _, err := os.Stat(filepath.Join(unknown, "AGENTS.md")); err == nil {
		t.Fatal("wrote AGENTS.md into a checkout whose remote the server does not know")
	}
	got := readFile(t, filepath.Join(known, "AGENTS.md"))
	if got != pair.agents.body {
		t.Fatal("known checkout did not receive AGENTS.md")
	}
	foundUnknown := false
	for _, r := range res.UnknownRemotes {
		if r == "github.com/acme/mystery" {
			foundUnknown = true
		}
	}
	if !foundUnknown {
		t.Fatalf("unknown remote not surfaced: %v", res.UnknownRemotes)
	}
}

func TestDiscoverTwoLevelsUpsertsWorkspaceAndSurfacesUnknownRemote(t *testing.T) {
	root := t.TempDir()
	known := filepath.Join(root, "proj", "known")
	unknown := filepath.Join(root, "proj", "unknown")
	shallow := filepath.Join(root, "shallow")
	tooDeep := filepath.Join(root, "a", "b", "c")
	initRepo(t, known, "https://github.com/acme/known.git")
	initRepo(t, unknown, "https://github.com/acme/mystery.git")
	initRepo(t, shallow, "https://github.com/acme/shallow.git")
	initRepo(t, tooDeep, "https://github.com/acme/deep.git")

	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	srv := newFake(t)
	srv.bound["github.com/acme/known"] = true
	srv.setTargets(homeTarget(claudeAt(t, "2026-05-01T00:00:00Z")))
	cfg := testConfig(home, state, srv.URL)
	cfg.Roots = []string{root}

	res, err := Sync(t.Context(), db, cfg)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}

	spaces, err := db.Workspaces()
	if err != nil {
		t.Fatal(err)
	}
	paths := map[string]Workspace{}
	for _, w := range spaces {
		paths[w.Path] = w
	}
	if _, ok := paths[known]; !ok {
		t.Fatalf("missing workspace for %s: %+v", known, spaces)
	}
	if _, ok := paths[unknown]; !ok {
		t.Fatalf("missing workspace for unknown checkout %s", unknown)
	}
	if _, ok := paths[shallow]; ok {
		t.Fatal("one-level checkout was discovered; EDD R26 is two levels")
	}
	if _, ok := paths[tooDeep]; ok {
		t.Fatal("three-level checkout was discovered; EDD R26 is two levels")
	}
	if paths[unknown].Remote != "github.com/acme/mystery" {
		t.Fatalf("remote = %q, want normalized github.com/acme/mystery", paths[unknown].Remote)
	}

	foundUnknown := false
	for _, r := range res.UnknownRemotes {
		if r == "github.com/acme/mystery" {
			foundUnknown = true
		}
		if r == "github.com/acme/known" {
			t.Fatal("bound remote was surfaced as unknown")
		}
	}
	if !foundUnknown {
		t.Fatalf("unknown remote not surfaced: %v", res.UnknownRemotes)
	}
	for _, req := range srv.requests() {
		if strings.Contains(req, "/v1/scope") || strings.Contains(req, "/v1/repo") {
			t.Fatalf("adapter created a scope for an unknown remote: %s", req)
		}
	}

	if _, err := Sync(t.Context(), db, cfg); err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	again, err := db.Workspaces()
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != len(spaces) {
		t.Fatalf("workspace upsert duplicated rows: first %d then %d", len(spaces), len(again))
	}
}

func TestSyncPrunesWorkspaceForRemovedCheckout(t *testing.T) {
	root := t.TempDir()
	keep := filepath.Join(root, "proj", "keep")
	gone := filepath.Join(root, "proj", "gone")
	initRepo(t, keep, "https://github.com/acme/keep.git")
	initRepo(t, gone, "https://github.com/acme/gone.git")

	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	srv := newFake(t)
	srv.bound["github.com/acme/keep"] = true
	srv.bound["github.com/acme/gone"] = true
	srv.setTargets(homeTarget(claudeAt(t, "2026-05-02T00:00:00Z")))
	cfg := testConfig(home, state, srv.URL)
	cfg.Roots = []string{root}

	if _, err := Sync(t.Context(), db, cfg); err != nil {
		t.Fatalf("first Sync: %v", err)
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(t.Context(), db, cfg); err != nil {
		t.Fatalf("second Sync: %v", err)
	}
	spaces, err := db.Workspaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, ws := range spaces {
		if ws.Path == gone || ws.Remote == "github.com/acme/gone" {
			t.Fatalf("removed checkout still in workspace: %+v", ws)
		}
	}
}

func TestSSEWakeRerendersWithoutWaitingForTick(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	first := claudeAt(t, "2026-06-01T00:00:00Z")
	second := claudeAtVersion(t, "2026-06-02T00:00:00Z", "3.14")

	srv := newFake(t)
	srv.setTargets(homeTarget(first))
	sseReady := make(chan struct{})
	wake := make(chan struct{})
	srv.sseReady = sseReady
	srv.sseWake = wake

	cfg := testConfig(home, state, srv.URL)
	cfg.Interval = time.Hour
	cfg.StatePath = state

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- Run(ctx, cfg) }()

	select {
	case <-sseReady:
	case err := <-errCh:
		t.Fatalf("Run exited before SSE: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("SSE never connected")
	}

	waitFile(t, filepath.Join(home, ".claude", "CLAUDE.md"), first.body, 5*time.Second)
	srv.setTargets(homeTarget(second))
	close(wake)

	waitFile(t, filepath.Join(home, ".claude", "CLAUDE.md"), second.body, 3*time.Second)
	cancel()
	select {
	case <-errCh:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not exit after cancel")
	}
}

func TestSyncRefusesHomePathOutsideRenderSpecs(t *testing.T) {
	home := t.TempDir()
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	body := "should-not-be-written\n"
	srv := newFake(t)
	srv.setTargets(target{Path: "~/.ssh/authorized_keys", Content: body, SHA256: render.DriftHash(body)})
	cfg := testConfig(home, state, srv.URL)
	if _, err := Sync(t.Context(), db, cfg); err == nil {
		t.Fatal("Sync accepted a ~/ path that is not in render.Specs()")
	} else if !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("error = %v, want a refusal naming the path", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".ssh", "authorized_keys")); err == nil {
		t.Fatal("wrote ~/.ssh/authorized_keys from a hostile /v1/render target")
	}
}

func TestSyncRefusesRelativePathOutsideRenderSpecs(t *testing.T) {
	home := t.TempDir()
	root := t.TempDir()
	repo := filepath.Join(root, "proj", "repo")
	initRepo(t, repo, "https://github.com/acme/known.git")
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	body := "SECRET=1\n"
	srv := newFake(t)
	srv.bound["github.com/acme/known"] = true
	srv.setTargets(target{Path: ".env", Content: body, SHA256: render.DriftHash(body)})
	cfg := testConfig(home, state, srv.URL)
	cfg.Roots = []string{root}
	if _, err := Sync(t.Context(), db, cfg); err == nil {
		t.Fatal("Sync accepted a relative path that is not in render.Specs()")
	} else if !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("error = %v, want a refusal naming the path", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".env")); err == nil {
		t.Fatal("wrote .env into a checkout from a hostile /v1/render target")
	}
}

func TestSyncRefusesDestDirSymlinkEscape(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(home, ".claude")); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(home, "adapter.sqlite")
	db := openDB(t, state)
	srv := newFake(t)
	srv.setTargets(homeTarget(claudeAt(t, "2026-08-01T00:00:00Z")))
	cfg := testConfig(home, state, srv.URL)
	if _, err := Sync(t.Context(), db, cfg); err == nil {
		t.Fatal("Sync followed a dest-dir symlink out of home")
	} else if !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("error = %v, want it to name the confinement refusal", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "CLAUDE.md")); err == nil {
		t.Fatal("wrote through a directory symlink that escaped home")
	}
}

func TestSyncRefusesAbsolutePathOutsideHomeAndRoots(t *testing.T) {
	body := "should-not-be-written\n"
	sum := render.DriftHash(body)
	cases := []struct {
		name    string
		path    string
		want    string
		setup   func(t *testing.T, home string)
		written func(home string) string
	}{
		{
			name: "absolute",
			path: "/etc/passwd",
			want: "refusing",
		},
		{
			name: "dot-dot",
			path: "../",
			want: "refusing",
		},
		{
			name: "tilde-traversal",
			path: "~/../../etc/passwd",
			want: "refusing",
		},
		{
			name: "home path not in specs",
			path: "~/.bashrc",
			want: "refusing",
			written: func(home string) string {
				return filepath.Join(home, ".bashrc")
			},
		},
		{
			name: "dest-dir symlink",
			path: render.HomeClaude,
			want: "escapes",
			setup: func(t *testing.T, home string) {
				t.Helper()
				outside := t.TempDir()
				if err := os.Symlink(outside, filepath.Join(home, ".claude")); err != nil {
					t.Fatal(err)
				}
			},
			written: func(home string) string {
				target, err := os.Readlink(filepath.Join(home, ".claude"))
				if err != nil {
					return ""
				}
				return filepath.Join(target, "CLAUDE.md")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			if tc.setup != nil {
				tc.setup(t, home)
			}
			state := filepath.Join(home, "adapter.sqlite")
			db := openDB(t, state)
			srv := newFake(t)
			if tc.path == render.HomeClaude {
				srv.setTargets(homeTarget(claudeAt(t, "2026-08-15T00:00:00Z")))
			} else {
				srv.setTargets(target{Path: tc.path, Content: body, SHA256: sum})
			}
			cfg := testConfig(home, state, srv.URL)
			_, err := Sync(t.Context(), db, cfg)
			if err == nil {
				t.Fatal("Sync accepted an unsafe target path")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q so an EACCES write is not mistaken for a refusal", err, tc.want)
			}
			if tc.written != nil {
				if p := tc.written(home); p != "" {
					if _, statErr := os.Stat(p); statErr == nil {
						t.Fatalf("wrote unsafe dest %s", p)
					}
				}
			}
		})
	}
}

func assertNoTemp(t *testing.T, dir string) {
	t.Helper()
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		name := info.Name()
		if strings.Contains(name, ".tmp") || strings.HasSuffix(name, ".tmp") || strings.Contains(name, "substrate-tmp") {
			t.Errorf("temp file survived: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func openDB(t *testing.T, path string) *DB {
	t.Helper()
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func testConfig(home, state, server string) Config {
	return Config{
		Server:    server,
		Token:     "test-token",
		Machine:   "test",
		Home:      home,
		StatePath: state,
		Scope:     "global:",
		Interval:  DefaultInterval,
	}
}

type rendered struct {
	body string
	hash string
}

func claudeAt(t *testing.T, at string) rendered {
	t.Helper()
	return claudeAtVersion(t, at, "3.12")
}

func claudeAtVersion(t *testing.T, at, version string) rendered {
	t.Helper()
	cfg := render.EffectiveConfig{
		Instructions: []instruction.Record{{
			Scope: "global:", Kind: "constraint", Key: "python.version", Body: version,
		}},
		GeneratedAt: at,
	}
	body, err := render.Render(render.TargetClaude, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return rendered{body: body, hash: render.DriftHash(body)}
}

func agentsAtVersion(t *testing.T, at, version string) rendered {
	t.Helper()
	cfg := render.EffectiveConfig{
		Instructions: []instruction.Record{{
			Scope: "global:", Kind: "constraint", Key: "python.version", Body: version,
		}},
		GeneratedAt: at,
	}
	body, err := render.Render(render.TargetAgents, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return rendered{body: body, hash: render.DriftHash(body)}
}

type pair struct {
	claude, agents rendered
	targets        []target
}

func pairAt(t *testing.T, at, version string) pair {
	t.Helper()
	c := claudeAtVersion(t, at, version)
	a := agentsAtVersion(t, at, version)
	return pair{
		claude: c,
		agents: a,
		targets: []target{
			{Path: render.HomeClaude, Content: c.body, SHA256: c.hash},
			{Path: render.RepoAgents, Content: a.body, SHA256: a.hash},
		},
	}
}

func homeTarget(r rendered) target {
	return target{Path: render.HomeClaude, Content: r.body, SHA256: r.hash}
}

func readHomeClaude(t *testing.T, home string) string {
	t.Helper()
	return readFile(t, filepath.Join(home, ".claude", "CLAUDE.md"))
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path) //nolint:gosec // path is under t.TempDir
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func waitFile(t *testing.T, path, want string, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	var last string
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(path) //nolint:gosec // path is under t.TempDir
		if err == nil {
			last = string(b)
			if last == want {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s to match server content (last len %d)", path, len(last))
}

func initRepo(t *testing.T, dir, origin string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "remote", "add", "origin", origin)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...) //nolint:gosec // argv is test literals; Dir is t.TempDir
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=test",
		"GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test",
		"GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

type target struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	SHA256  string `json:"sha256"`
}

type reviewReq struct {
	Kind string
	Diff string
}

type fake struct {
	mu           sync.Mutex
	targets      []target
	bound        map[string]bool
	posted       []reviewReq
	httpReqs     []string
	reviewHook   func()
	reviewStatus int
	sseReady     chan struct{}
	sseWake      <-chan struct{}
	URL          string
}

func newFake(t *testing.T) *fake {
	t.Helper()
	f := &fake{bound: map[string]bool{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/render", f.handleRender)
	mux.HandleFunc("POST /v1/review", f.handleReview)
	mux.HandleFunc("GET /v1/events", f.handleEvents)
	mux.HandleFunc("/", f.handleOther)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	f.URL = srv.URL
	return f
}

func (f *fake) setTargets(ts ...target) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.targets = append([]target(nil), ts...)
}

func (f *fake) reviewCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.posted)
}

func (f *fake) reviews() []reviewReq {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]reviewReq, len(f.posted))
	copy(out, f.posted)
	return out
}

func (f *fake) requests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.httpReqs))
	copy(out, f.httpReqs)
	return out
}

func (f *fake) note(r *http.Request) {
	f.mu.Lock()
	f.httpReqs = append(f.httpReqs, r.Method+" "+r.URL.Path)
	f.mu.Unlock()
}

func (f *fake) handleRender(w http.ResponseWriter, r *http.Request) {
	f.note(r)
	repos := splitCSV(r.URL.Query().Get("repos"))
	f.mu.Lock()
	targets := append([]target(nil), f.targets...)
	bound := f.bound
	f.mu.Unlock()
	for _, repo := range repos {
		if len(bound) > 0 && !bound[repo] {
			http.Error(w, `{"code":"SUBSTRATE_DENIED_SCOPE"}`, http.StatusForbidden)
			return
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"targets": targets})
}

func (f *fake) handleReview(w http.ResponseWriter, r *http.Request) {
	f.note(r)
	if f.reviewHook != nil {
		f.reviewHook()
	}
	if f.reviewStatus != 0 && f.reviewStatus != http.StatusOK && f.reviewStatus != http.StatusCreated {
		http.Error(w, "review failed", f.reviewStatus)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var in struct {
		Kind    string `json:"kind"`
		Payload struct {
			Diff string `json:"diff"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(body, &in); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	f.mu.Lock()
	f.posted = append(f.posted, reviewReq{Kind: in.Kind, Diff: in.Payload.Diff})
	f.mu.Unlock()
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"id": "rev-1", "kind": in.Kind, "status": "open"})
}

func (f *fake) handleEvents(w http.ResponseWriter, r *http.Request) {
	f.note(r)
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no flush", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	if f.sseReady != nil {
		select {
		case <-f.sseReady:
		default:
			close(f.sseReady)
		}
	}
	if f.sseWake != nil {
		select {
		case <-f.sseWake:
			if _, err := fmt.Fprintf(w, "data: {}\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
	<-r.Context().Done()
}

func (f *fake) handleOther(w http.ResponseWriter, r *http.Request) {
	f.note(r)
	http.NotFound(w, r)
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
