package artifacts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ncruces/go-sqlite3"
	"github.com/ncruces/go-sqlite3/driver"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCancelledIndexLeavesWorkQueuedWithoutFailure(t *testing.T) {
	f := setup(t)
	capture(t, f.session, memory("cancel", "cancelled indexing"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.store.IndexNext(ctx, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation misclassified: %v", err)
	}
	state, err := f.store.Maintenance()
	if err != nil || state.Failed != 0 || state.Queued != 1 {
		t.Fatalf("cancel changed queue %+v %v", state, err)
	}
}

func TestCancellationDuringCheckpointRollsBackPostings(t *testing.T) {
	f := setup(t)
	capture(t, f.session, memory("interrupt", "unfinished tokens"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn, err := f.store.db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	err = conn.Raw(func(raw any) error {
		return raw.(driver.Conn).Raw().CreateFunction("cancel_index", 0, sqlite3.INNOCUOUS, func(sqlite3.Context, ...sqlite3.Value) { cancel() })
	})
	conn.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.db.Exec("CREATE TRIGGER cancel_checkpoint BEFORE INSERT ON indexed BEGIN SELECT cancel_index(); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.IndexNext(ctx, false); !errors.Is(err, context.Canceled) {
		t.Fatalf("checkpoint cancellation misclassified %v", err)
	}
	var postings, checkpoints int
	if err := f.store.db.QueryRow("SELECT count(*) FROM tokens").Scan(&postings); err != nil {
		t.Fatal(err)
	}
	if err := f.store.db.QueryRow("SELECT count(*) FROM indexed").Scan(&checkpoints); err != nil {
		t.Fatal(err)
	}
	state, err := f.store.Maintenance()
	if err != nil || postings != 0 || checkpoints != 0 || state.Failed != 0 || state.Queued != 1 {
		t.Fatalf("cancelled transaction escaped: postings%d checkpoints%d %+v %v", postings, checkpoints, state, err)
	}
}

func TestRapidEditsCoalesceNewestHeadAndPreserveOperationLedger(t *testing.T) {
	f := setup(t)
	current := capture(t, f.session, memory("coalesce-first", "superseded content"))
	for i := 0; i < 8; i++ {
		edit := memory(fmt.Sprint("coalesce-", i), fmt.Sprint("currentneedle", i))
		edit.ArtifactID, edit.ExpectedRevision = current.ArtifactID, current.RevisionID
		current = capture(t, f.session, edit)
	}
	before, err := f.session.Pending()
	if err != nil {
		t.Fatal(err)
	}
	state, err := f.store.Maintenance()
	if err != nil || state.Queued != 1 {
		t.Fatalf("edits did not coalesce %+v %v", state, err)
	}
	if processed, err := f.store.IndexBatch(100); err != nil || processed != 1 {
		t.Fatalf("multiple jobs for one artifact %d %v", processed, err)
	}
	got, err := f.session.Search(SearchRequest{Query: "currentneedle7"})
	if err != nil || len(got.Results) != 1 || got.Results[0].RevisionID != current.RevisionID {
		t.Fatalf("coalesced job indexed stale head %+v %v", got, err)
	}
	after, err := f.session.Pending()
	if err != nil || !reflect.DeepEqual(before, after) || len(after) != 9 {
		t.Fatalf("coalescing lost operations %v %v", after, err)
	}
}

func TestHistoryOnlyContributionPreservesDiscretionaryRebuild(t *testing.T) {
	for _, indexed := range []bool{false, true} {
		t.Run(fmt.Sprint(indexed), func(t *testing.T) {
			f := setup(t)
			first := capture(t, f.session, memory("first", "superseded evidence"))
			edit := memory("current", "repairneedle")
			edit.ArtifactID, edit.ExpectedRevision = first.ArtifactID, first.RevisionID
			current := capture(t, f.session, edit)
			if indexed {
				if _, err := f.store.IndexBatch(100); err != nil {
					t.Fatal(err)
				}
				if _, err := f.store.db.Exec("DELETE FROM tokens"); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.store.RebuildIndex(); err != nil {
				t.Fatal(err)
			}
			stale := memory("stale", "history only")
			stale.ArtifactID, stale.ExpectedRevision = first.ArtifactID, first.RevisionID
			capture(t, f.session, stale)
			state, err := f.store.Maintenance()
			if err != nil || state.Queued != 0 || state.Deferred != 1 {
				t.Fatalf("history discarded bulk intent %+v %v", state, err)
			}
			if attempted, err := f.store.IndexNext(context.Background(), false); err != nil || attempted {
				t.Fatalf("automatic consumed discretionary job %v %v", attempted, err)
			}
			if attempted, err := f.store.IndexNext(context.Background(), true); err != nil || !attempted {
				t.Fatalf("explicit rebuild lost %v %v", attempted, err)
			}
			got, err := f.session.Search(SearchRequest{Query: "repairneedle"})
			if err != nil || len(got.Results) != 1 || got.Results[0].RevisionID != current.RevisionID {
				t.Fatalf("repair skipped current head %+v %v", got, err)
			}
			if err := f.store.RebuildIndex(); err != nil {
				t.Fatal(err)
			}
			changed := memory("changed", "new current content")
			changed.ArtifactID, changed.ExpectedRevision = current.ArtifactID, current.RevisionID
			capture(t, f.session, changed)
			state, err = f.store.Maintenance()
			if err != nil || state.Queued != 1 || state.Deferred != 0 {
				t.Fatalf("changed head remained discretionary %+v %v", state, err)
			}
		})
	}
}

func TestBulkRebuildIsDeferredAndRegeneratesMatchingCheckpoint(t *testing.T) {
	f := setup(t)
	r := capture(t, f.session, memory("bulk", "rebuild otter"))
	if _, err := f.store.IndexBatch(100); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.db.Exec("DELETE FROM tokens WHERE artifact_id=?", r.ArtifactID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.RebuildIndex(); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	f.store, err = Open(f.auth)
	if err != nil {
		t.Fatal(err)
	}
	f.session, err = f.store.Session(f.token, f.checkout)
	if err != nil {
		t.Fatal(err)
	}
	if attempted, err := f.store.IndexNext(context.Background(), false); err != nil || attempted {
		t.Fatalf("automatic work consumed bulk: %v %v", attempted, err)
	}
	state, err := f.store.Maintenance()
	if err != nil || state.State != "deferred" || state.Deferred != 1 {
		t.Fatalf("lost deferred state %+v %v", state, err)
	}
	if attempted, err := f.store.IndexNext(context.Background(), true); err != nil || !attempted {
		t.Fatalf("explicit bulk failed %v %v", attempted, err)
	}
	got, err := f.session.Search(SearchRequest{Query: "otter"})
	if err != nil || len(got.Results) != 1 {
		t.Fatalf("matching checkpoint prevented regeneration %+v %v", got, err)
	}
}

func TestScopedMaintenanceCountsBeyondInventoryLimitAndHiddenFailures(t *testing.T) {
	f := setup(t)
	for i := 0; i < 103; i++ {
		capture(t, f.session, memory(fmt.Sprint("scope-", i), "visible observation"))
	}
	before, err := f.session.BrowserInventory()
	if err != nil || len(before.Artifacts) != 100 || before.Maintenance.Queued != 103 || before.Maintenance.Coverage.Eligible != 103 {
		t.Fatalf("bounded list lost scoped counts %+v %v", before.Maintenance, err)
	}
	if err := f.auth.CreateSpace("Work"); err != nil {
		t.Fatal(err)
	}
	checkout := filepath.Join(t.TempDir(), "work")
	if err := os.Mkdir(checkout, 0700); err != nil {
		t.Fatal(err)
	}
	git(t, checkout, "init", "-q")
	if _, err := f.auth.Register(checkout, "Work"); err != nil {
		t.Fatal(err)
	}
	token, err := f.auth.CreateSession(checkout, "Work")
	if err != nil {
		t.Fatal(err)
	}
	work, err := f.store.Session(token, checkout)
	if err != nil {
		t.Fatal(err)
	}
	hidden := capture(t, work, memory("hidden", "hidden material"))
	if _, err := f.store.db.Exec("UPDATE index_queue SET failure='indexing failed',bulk=1 WHERE artifact_id=?", hidden.ArtifactID); err != nil {
		t.Fatal(err)
	}
	after, err := f.session.BrowserInventory()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("hidden work changed Personal inventory: before %+v after %+v %v", before.Maintenance, after.Maintenance, err)
	}
}

func TestFailedWorkSurvivesEditUntilExplicitRetry(t *testing.T) {
	f := setup(t)
	r := capture(t, f.session, memory("fail", "first failure"))
	if _, err := f.store.db.Exec("CREATE TRIGGER fail_index BEFORE INSERT ON indexed BEGIN SELECT RAISE(ABORT,'injected'); END"); err != nil {
		t.Fatal(err)
	}
	f.store.IndexBatch(100)
	edit := memory("edited", "edited healthy content")
	edit.ArtifactID, edit.ExpectedRevision = r.ArtifactID, r.RevisionID
	capture(t, f.session, edit)
	f.store.db.Exec("DROP TRIGGER fail_index")
	if processed, err := f.store.IndexBatch(100); processed != 0 || err != nil {
		t.Fatalf("edit silently retried failure %d %v", processed, err)
	}
	if err := f.store.RetryIndex(); err != nil {
		t.Fatal(err)
	}
	if processed, err := f.store.IndexBatch(100); processed != 1 || err != nil {
		t.Fatalf("explicit retry failed %d %v", processed, err)
	}
}

func TestFailedAttemptsCountTowardBatchBound(t *testing.T) {
	f := setup(t)
	for i := 0; i < 105; i++ {
		capture(t, f.session, memory(fmt.Sprint("poison-", i), "poison job"))
	}
	if _, err := f.store.db.Exec("CREATE TRIGGER fail_index BEFORE INSERT ON indexed BEGIN SELECT RAISE(ABORT,'injected'); END"); err != nil {
		t.Fatal(err)
	}
	if processed, err := f.store.IndexBatch(100); processed != 0 || !errors.Is(err, ErrUnavailable) {
		t.Fatalf("poison result %d %v", processed, err)
	}
	state, err := f.store.Maintenance()
	if err != nil || state.Failed != 100 || state.Queued != 5 {
		t.Fatalf("attempt limit bypassed %+v %v", state, err)
	}
}

func TestVersionFourMigrationPreservesDurableReceiptsAndPause(t *testing.T) {
	f := setup(t)
	r := capture(t, f.session, memory("migration", "legacy content"))
	if _, err := f.store.db.Exec("DROP TABLE maintenance; ALTER TABLE index_queue DROP COLUMN bulk; ALTER TABLE index_queue DROP COLUMN failure; PRAGMA user_version=4"); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	var err error
	f.store, err = Open(f.auth)
	if err != nil {
		t.Fatal(err)
	}
	f.session, err = f.store.Session(f.token, f.checkout)
	if err != nil {
		t.Fatal(err)
	}
	if retry := capture(t, f.session, memory("migration", "legacy content")); retry != r {
		t.Fatalf("migration changed receipt %+v %+v", r, retry)
	}
	if err := f.store.SetIndexPaused(true); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	f.store, err = Open(f.auth)
	if err != nil {
		t.Fatal(err)
	}
	state, err := f.store.Maintenance()
	if err != nil || !state.Paused || state.Queued != 1 || state.Deferred != 0 || state.Failed != 0 {
		t.Fatalf("migration lost durable state %+v %v", state, err)
	}
}

func TestUnchangedHeadConsumesQueueWithoutRewritingPostings(t *testing.T) {
	f := setup(t)
	first := capture(t, f.session, memory("first", "accepted otter"))
	if _, err := f.store.IndexBatch(100); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.db.Exec("CREATE TRIGGER deny_rewrite BEFORE DELETE ON tokens BEGIN SELECT RAISE(ABORT,'unchanged rewrite'); END"); err != nil {
		t.Fatal(err)
	}
	edit := memory("current", "current otter")
	edit.ArtifactID, edit.ExpectedRevision = first.ArtifactID, first.RevisionID
	current := capture(t, f.session, edit)
	f.store.db.Exec("DROP TRIGGER deny_rewrite")
	if _, err := f.store.IndexBatch(100); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.db.Exec("CREATE TRIGGER deny_rewrite BEFORE DELETE ON tokens BEGIN SELECT RAISE(ABORT,'unchanged rewrite'); END"); err != nil {
		t.Fatal(err)
	}
	stale := memory("stale", "history only")
	stale.ArtifactID, stale.ExpectedRevision = first.ArtifactID, first.RevisionID
	if r := capture(t, f.session, stale); r.State != "conflict" {
		t.Fatalf("unexpected stale receipt %+v", r)
	}
	if _, err := f.store.IndexBatch(100); err != nil {
		t.Fatalf("history-only contribution rewrote head %s: %v", current.RevisionID, err)
	}
}

func TestFailedIndexRemainsFailedAndHealthyWorkProgresses(t *testing.T) {
	f := setup(t)
	bad := capture(t, f.session, memory("bad", "failed otter"))
	if _, err := f.store.db.Exec("CREATE TRIGGER fail_one BEFORE INSERT ON tokens WHEN NEW.artifact_id='" + bad.ArtifactID + "' BEGIN SELECT RAISE(ABORT,'private detail'); END"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.IndexBatch(100); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("missing failure: %v", err)
	}
	capture(t, f.session, memory("healthy", "healthy zebra"))
	if _, err := f.store.IndexBatch(100); err != nil {
		t.Fatalf("failed work blocked healthy progress: %v", err)
	}
	got, err := f.session.Search(SearchRequest{Query: "zebra"})
	if err != nil || len(got.Results) != 1 {
		t.Fatalf("healthy work not indexed %+v %v", got, err)
	}
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	f.store, err = Open(f.auth)
	if err != nil {
		t.Fatal(err)
	}
	f.session, err = f.store.Session(f.token, f.checkout)
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := f.session.BrowserInventory()
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(inventory)
	var result struct {
		Maintenance struct {
			State          string
			Failed, Queued int
		}
	}
	if err := json.Unmarshal(encoded, &result); err != nil {
		t.Fatal(err)
	}
	if result.Maintenance.State != "failed" || result.Maintenance.Failed != 1 || result.Maintenance.Queued != 0 {
		t.Fatalf("failure missing after restart: %s", encoded)
	}
}
