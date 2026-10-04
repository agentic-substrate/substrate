package artifacts

import (
	"fmt"
	"github.com/agentic-substrate/substrate/internal/authority"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestRepresentativeQueryEvaluation(t *testing.T) {
	f := setup(t)
	build := memory("build", "Run npm run build:web before go build for embedded UI.")
	build.Associations = Associations{Identifiers: []string{"BUILD-42"}, Aliases: []string{"frontend compilation"}, Topics: []string{"build"}}
	b := capture(t, f.session, build)
	auth := memory("auth", "Session credentials expire after 24 hours. Revocation rejects future reads.")
	auth.Associations.Topics = []string{"security"}
	a := capture(t, f.session, auth)
	storage := capture(t, f.session, memory("storage", "SQLite commits content, retry receipt, and pending work atomically."))
	skill := gitCandidate(t, f, "eval-skill", "", "", "Approved build skill uses npm run build:web.")
	owner, _ := registered(t, f, skill, "repo", "build-eval", false)
	approve(t, owner, skill, "")
	if err := os.WriteFile(filepath.Join(f.checkout, "AGENT.md"), []byte("Approved agent definition for code review."), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, f.checkout, "add", "AGENT.md")
	git(t, f.checkout, "-c", "user.name=Owner", "-c", "user.email=owner@example.test", "commit", "-qm", "definition")
	definition := capture(t, f.session, Contribution{OperationID: "eval-agent", Kind: "agent-definition", Source: &Source{Commit: git(t, f.checkout, "rev-parse", "HEAD"), Path: "AGENT.md"}})
	owner, _ = registered(t, f, definition, "repo", "review-eval", false)
	approve(t, owner, definition, "")
	start := time.Now()
	if _, err := f.store.IndexBatch(100); err != nil {
		t.Fatal(err)
	}
	t.Logf("five revision cold index: %s", time.Since(start))
	cases := []struct{ query, want string }{{"BUILD-42", b.ArtifactID}, {"frontend compilation", b.ArtifactID}, {"compil*", b.ArtifactID}, {"security", a.ArtifactID}, {"retry receipt", storage.ArtifactID}, {"code review", definition.ArtifactID}, {skill.RevisionID, skill.ArtifactID}, {"complation", ""}, {"prepare painted interface", ""}}
	for _, test := range cases {
		t.Run(test.query, func(t *testing.T) {
			start := time.Now()
			got, err := f.session.Search(SearchRequest{Query: test.query})
			elapsed := time.Since(start)
			if err != nil {
				t.Fatal(err)
			}
			if test.want == "" {
				if len(got.Results) != 0 {
					t.Fatalf("unexpected lexical inference %+v", got.Results)
				}
			} else if len(got.Results) == 0 || got.Results[0].ArtifactID != test.want {
				t.Fatalf("wrong leading result %+v", got.Results)
			}
			t.Logf("query latency=%s results=%d", elapsed, len(got.Results))
		})
	}
}

func BenchmarkScopedLexicalSearch(b *testing.B) {
	dir := b.TempDir()
	checkout := filepath.Join(dir, "repo")
	os.Mkdir(checkout, 0700)
	if out, err := exec.Command("git", "-C", checkout, "init", "-q").CombinedOutput(); err != nil {
		b.Fatalf("git %v %s", err, out)
	}
	auth := &authority.Store{Dir: filepath.Join(dir, "state")}
	if err := auth.Initialize("Owner"); err != nil {
		b.Fatal(err)
	}
	if _, err := auth.Register(checkout, "Personal"); err != nil {
		b.Fatal(err)
	}
	token, err := auth.CreateSession(checkout, "Personal")
	if err != nil {
		b.Fatal(err)
	}
	store, err := Open(auth)
	if err != nil {
		b.Fatal(err)
	}
	defer store.Close()
	session, err := store.Session(token, checkout)
	if err != nil {
		b.Fatal(err)
	}
	for i := range 256 {
		c := memory(fmt.Sprintf("capture-%d", i), fmt.Sprintf("Synthetic repository observation %d: compile frontend assets before building Go; preserve exact revisions and authorization for local sessions. SQLite retries use immutable operation receipts.", i))
		c.Associations = Associations{Identifiers: []string{fmt.Sprintf("OBS-%d", i)}, Topics: []string{"development"}}
		if _, err := session.Contribute(c); err != nil {
			b.Fatal(err)
		}
	}
	start := time.Now()
	for {
		count, err := store.IndexBatch(100)
		if err != nil {
			b.Fatal(err)
		}
		if count == 0 {
			break
		}
	}
	b.Logf("256 revisions cold index=%s", time.Since(start))
	queries := []string{"compile frontend", "OBS-42", "immutable receipts", "develop*", "unknown paraphrase"}
	start = time.Now()
	if _, err := session.Search(SearchRequest{Query: queries[0]}); err != nil {
		b.Fatal(err)
	}
	b.Logf("first query=%s", time.Since(start))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := session.Search(SearchRequest{Query: queries[i%len(queries)]}); err != nil {
			b.Fatal(err)
		}
	}
}
