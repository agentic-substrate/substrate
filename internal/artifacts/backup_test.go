package artifacts

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentic-substrate/substrate/internal/authority"
	"github.com/ncruces/go-sqlite3/driver"
)

func backupScope(t *testing.T, auth *authority.Store) authority.BackupScope {
	t.Helper()
	var scope authority.BackupScope
	if err := auth.WithBackupSnapshot(func(_ []byte, s authority.BackupScope) error { scope = s; return nil }); err != nil {
		t.Fatal(err)
	}
	return scope
}

func TestSnapshotDoesNotMigrateOriginalSchema(t *testing.T) {
	auth := &authority.Store{Dir: filepath.Join(t.TempDir(), "state?x=#")}
	if err := auth.Initialize("Owner"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(auth.Dir, "artifacts.db")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := driver.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "snapshot?mode=ro#db")
	version, err := Snapshot(path, destination, backupScope(t, auth))
	if err != nil {
		t.Fatal(err)
	}
	if version != 1 {
		t.Fatalf("source version migrated: %d", version)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("source bytes changed")
	}
	if err := ValidateBackup(destination, backupScope(t, auth), 1); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotPreservesDurableMaintenance(t *testing.T) {
	f := setup(t)
	first := capture(t, f.session, memory("first", "indexed observation"))
	second := capture(t, f.session, memory("second", "failed observation"))
	third := capture(t, f.session, memory("third", "queued observation"))
	if _, err := f.store.IndexBatch(100); err != nil {
		t.Fatal(err)
	}
	if err := f.store.RebuildIndex(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.db.Exec("UPDATE index_queue SET failure='indexing failed' WHERE artifact_id=?", second.ArtifactID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.db.Exec("UPDATE index_queue SET bulk=0 WHERE artifact_id=?", third.ArtifactID); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetIndexPaused(true); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "snapshot")
	version, err := Snapshot(filepath.Join(f.auth.Dir, "artifacts.db"), destination, backupScope(t, f.auth))
	if err != nil || version != 5 {
		t.Fatalf("durable maintenance snapshot: version%d %v", version, err)
	}
	db, err := openBackup(destination)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var paused bool
	if err := db.QueryRow("SELECT paused FROM maintenance WHERE singleton=1").Scan(&paused); err != nil || !paused {
		t.Fatalf("pause lost: %v %v", paused, err)
	}
	for _, check := range []struct {
		id      string
		bulk    bool
		failure string
	}{{first.ArtifactID, true, ""}, {second.ArtifactID, true, "indexing failed"}, {third.ArtifactID, false, ""}} {
		var bulk bool
		var failure string
		if err := db.QueryRow("SELECT bulk,failure FROM index_queue WHERE artifact_id=?", check.id).Scan(&bulk, &failure); err != nil || bulk != check.bulk || failure != check.failure {
			t.Fatalf("durable job changed: %s %v %q %v", check.id, bulk, failure, err)
		}
	}
}

func TestBackupRejectsInvalidDurableMaintenance(t *testing.T) {
	for name, query := range map[string]string{
		"missing state": "DELETE FROM maintenance",
		"failure":       "UPDATE index_queue SET failure='private forged diagnostics'",
	} {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			capture(t, f.session, memory("saved", "saved observation"))
			if _, err := f.store.db.Exec(query); err != nil {
				t.Fatal(err)
			}
			if err := ValidateBackup(filepath.Join(f.auth.Dir, "artifacts.db"), backupScope(t, f.auth), 5); err == nil {
				t.Fatal("invalid maintenance state accepted")
			}
		})
	}
}

func TestBackupRejectsExecutableSchemaAndBrokenHeads(t *testing.T) {
	for name, query := range map[string]string{
		"trigger": "CREATE TRIGGER injected AFTER INSERT ON artifacts BEGIN DELETE FROM revisions; END",
		"view":    "CREATE VIEW extra AS SELECT * FROM artifacts",
		"head":    "UPDATE artifacts SET head='absent'",
		"owner":   "UPDATE artifacts SET owner_id='wrong-owner'",
		"source":  "UPDATE revisions SET source='{}'",
	} {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			capture(t, f.session, memory("saved", "durable"))
			if _, err := f.store.db.Exec(query); err != nil {
				t.Fatal(err)
			}
			scope := backupScope(t, f.auth)
			if err := ValidateBackup(filepath.Join(f.auth.Dir, "artifacts.db"), scope, 5); err == nil {
				t.Fatal("invalid application state accepted")
			}
		})
	}
}

func TestSnapshotRejectsMissingHotAndOversizedDatabase(t *testing.T) {
	f := setup(t)
	path := filepath.Join(f.auth.Dir, "artifacts.db")
	scope := backupScope(t, f.auth)
	if _, err := Snapshot(path+"missing", filepath.Join(t.TempDir(), "out"), scope); err == nil {
		t.Fatal("missing database accepted")
	}
	if err := os.WriteFile(path+"-journal", []byte("incomplete write"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Snapshot(path, filepath.Join(t.TempDir(), "out"), scope); err == nil || !strings.Contains(err.Error(), "journal") {
		t.Fatalf("hot database accepted: %v", err)
	}
	if err := os.Remove(path + "-journal"); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, MaxBackupDatabase+1); err != nil {
		t.Fatal(err)
	}
	if _, err := Snapshot(path, filepath.Join(t.TempDir(), "out"), scope); err == nil || !strings.Contains(err.Error(), "1 GiB") {
		t.Fatalf("oversize database accepted: %v", err)
	}
}

func TestBackupPreservesDeferredPublicationDestinationAndHeadlessCheckpoint(t *testing.T) {
	f := publicationSetup(t)
	f.permit(t)
	in := f.input()
	in.Destination = PublicationDestination{strings.Repeat("z", 64), strings.Repeat("y", 64)}
	if _, err := f.work.ProposePublication(in); err != nil {
		t.Fatal(err)
	}
	root := f.checkout
	if err := os.WriteFile(filepath.Join(root, "skill.md"), []byte("Use the documented checks."), 0600); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", "skill.md")
	git(t, root, "-c", "user.name=Owner", "-c", "user.email=owner@example.test", "commit", "-qm", "fixture")
	capture(t, f.session, Contribution{OperationID: "candidate", Kind: "skill", Source: &Source{Commit: git(t, root, "rev-parse", "HEAD"), Path: "skill.md"}})
	if _, err := f.store.IndexBatch(100); err != nil {
		t.Fatal(err)
	}
	if err := ValidateBackup(filepath.Join(f.auth.Dir, "artifacts.db"), backupScope(t, f.auth), 5); err != nil {
		t.Fatalf("legitimate deferred state rejected: %v", err)
	}
}

func TestBackupPreservesLongUnknownStaleBase(t *testing.T) {
	f := setup(t)
	first := capture(t, f.session, memory("first", "current"))
	c := memory("stale", "preserved stale observation")
	c.ArtifactID = first.ArtifactID
	c.ExpectedRevision = strings.Repeat("unknown", 100)
	if receipt := capture(t, f.session, c); receipt.State != "conflict" {
		t.Fatalf("unexpected stale state: %+v", receipt)
	}
	if err := ValidateBackup(filepath.Join(f.auth.Dir, "artifacts.db"), backupScope(t, f.auth), 5); err != nil {
		t.Fatalf("acknowledged stale base rejected: %v", err)
	}
}

func TestBackupRejectsCorruptIndexPostings(t *testing.T) {
	for name, query := range map[string]string{
		"missing":    "DELETE FROM tokens WHERE position=0",
		"altered":    "UPDATE tokens SET token='forged' WHERE position=0",
		"duplicate":  "INSERT INTO tokens SELECT * FROM tokens WHERE position=0",
		"position":   "UPDATE tokens SET position=position+1",
		"checkpoint": "DELETE FROM indexed",
		"limit":      "UPDATE indexed SET limited=1",
	} {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			capture(t, f.session, memory("saved", "durable observation"))
			if _, err := f.store.IndexBatch(100); err != nil {
				t.Fatal(err)
			}
			if _, err := f.store.db.Exec(query); err != nil {
				t.Fatal(err)
			}
			if err := ValidateBackup(filepath.Join(f.auth.Dir, "artifacts.db"), backupScope(t, f.auth), 5); err == nil {
				t.Fatal("corrupt index postings accepted")
			}
		})
	}
}

func TestBackupPreservesStaleIndexCheckpoint(t *testing.T) {
	f := setup(t)
	first := capture(t, f.session, memory("first", "historical indexed content"))
	if _, err := f.store.IndexBatch(100); err != nil {
		t.Fatal(err)
	}
	c := memory("next", "new content awaiting maintenance")
	c.ArtifactID, c.ExpectedRevision = first.ArtifactID, first.RevisionID
	capture(t, f.session, c)
	if err := ValidateBackup(filepath.Join(f.auth.Dir, "artifacts.db"), backupScope(t, f.auth), 5); err != nil {
		t.Fatalf("valid historical checkpoint rejected: %v", err)
	}
}

func TestBackupRejectsBrokenRelatedScope(t *testing.T) {
	for _, crossScope := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "cross scope"}[crossScope], func(t *testing.T) {
			f := publicationSetup(t)
			related := strings.Repeat("0", 64)
			if crossScope {
				related = capture(t, f.session, memory("personal", "private personal observation")).ArtifactID
			}
			if _, err := f.store.db.Exec("UPDATE revision_associations SET metadata=json_set(metadata,'$.related',json_array(?)) WHERE revision_id=?", related, f.source.RevisionID); err != nil {
				t.Fatal(err)
			}
			if err := ValidateBackup(filepath.Join(f.auth.Dir, "artifacts.db"), backupScope(t, f.auth), 5); err == nil {
				t.Fatal("broken related endpoint accepted")
			}
		})
	}
}

func TestBackupPreservesRetiredRelatedEndpoint(t *testing.T) {
	f := setup(t)
	target := capture(t, f.session, memory("target", "related observation"))
	c := memory("source", "source observation")
	c.Associations.Related = []string{target.ArtifactID}
	capture(t, f.session, c)
	if _, err := f.session.Retire(target.ArtifactID, target.RevisionID, "retire"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateBackup(filepath.Join(f.auth.Dir, "artifacts.db"), backupScope(t, f.auth), 5); err != nil {
		t.Fatalf("valid retired relation rejected: %v", err)
	}
}

func TestBackupRejectsInvalidPendingOperationAndAction(t *testing.T) {
	for name, query := range map[string]string{"operation": "UPDATE pending SET operation_id='wrong operation'", "action": "UPDATE pending SET action='execute'"} {
		t.Run(name, func(t *testing.T) {
			f := setup(t)
			capture(t, f.session, memory("save", "saved"))
			if _, err := f.store.db.Exec(query); err != nil {
				t.Fatal(err)
			}
			if err := ValidateBackup(filepath.Join(f.auth.Dir, "artifacts.db"), backupScope(t, f.auth), 5); err == nil {
				t.Fatal("forged pending work accepted")
			}
		})
	}
}

func TestBackupRejectsBrokenCompletedPublicationAudit(t *testing.T) {
	for name, query := range map[string]string{
		"recipient":      `UPDATE publication_proposals SET review=json_set(review,'$.destination_context.repo_id','forged')`,
		"receipt":        `UPDATE publication_proposals SET receipt=json_set(receipt,'$.artifact_id','forged')`,
		"audit snapshot": `UPDATE publication_proposals SET review=json_set(review,'$.audience','forged')`,
	} {
		t.Run(name, func(t *testing.T) {
			f := publicationSetup(t)
			_, token, review := f.review(t)
			if _, err := f.store.Publish(token, approval(review)); err != nil {
				t.Fatal(err)
			}
			if _, err := f.store.db.Exec(query); err != nil {
				t.Fatal(err)
			}
			if err := ValidateBackup(filepath.Join(f.auth.Dir, "artifacts.db"), backupScope(t, f.auth), 5); err == nil {
				t.Fatal("invalid completed publication audit accepted")
			}
		})
	}
}

func TestRestoredStageClearsConsumedReviewTokensAndPreservesAudit(t *testing.T) {
	f := publicationSetup(t)
	_, token, review := f.review(t)
	receipt, err := f.store.Publish(token, approval(review))
	if err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(t.TempDir(), "stage")
	if err := os.Mkdir(stage, 0700); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(f.auth.Dir, "authority.json"))
	if err != nil {
		t.Fatal(err)
	}
	data, err = authority.ResetBackupSessions(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "authority.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Snapshot(filepath.Join(f.auth.Dir, "artifacts.db"), filepath.Join(stage, "artifacts.db"), backupScope(t, f.auth)); err != nil {
		t.Fatal(err)
	}
	if err := PrepareRestored(&authority.Store{Dir: stage}); err != nil {
		t.Fatal(err)
	}
	store, err := Open(&authority.Store{Dir: stage})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Publish(token, approval(review)); !errors.Is(err, authority.ErrDenied) {
		t.Fatalf("old consumed token allowed retry: %v", err)
	}
	var encoded, audit string
	if err := store.db.QueryRow("SELECT receipt,review FROM publication_proposals WHERE id=?", review.Proposal.ID).Scan(&encoded, &audit); err != nil {
		t.Fatal(err)
	}
	var recovered Receipt
	if err := json.Unmarshal([]byte(encoded), &recovered); err != nil {
		t.Fatal(err)
	}
	if recovered != receipt || audit == "" {
		t.Fatalf("private receipt or audit lost: %s %s", encoded, audit)
	}
}

func TestRestoreStageMigrationFailureRetainsOriginalVersion(t *testing.T) {
	auth := &authority.Store{Dir: filepath.Join(t.TempDir(), "stage")}
	if err := auth.Initialize("Owner"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(auth.Dir, "artifacts.db")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	db, err := driver.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(schema + "CREATE TABLE registrations(blocks_migration TEXT)"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := PrepareRestored(auth); err == nil {
		t.Fatal("failed migration reported complete")
	}
	db, err = driver.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 1 {
		t.Fatalf("failed migration advanced schema: %d", version)
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name='approvals'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial migration persisted: %d %v", count, err)
	}
}
