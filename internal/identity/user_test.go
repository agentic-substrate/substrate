package identity

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// failAtExec fails the Nth Exec and records the ones it let through, so a
// caller can assert exactly how far CreateUser got before it gave up.
type failAtExec struct {
	failOn int
	seen   []string
}

var errInjected = errors.New("injected write failure")

func (f *failAtExec) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	f.seen = append(f.seen, sql)
	if len(f.seen) == f.failOn {
		return pgconn.CommandTag{}, errInjected
	}
	return pgconn.CommandTag{}, nil
}

func (f *failAtExec) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query")
}

func (f *failAtExec) QueryRow(context.Context, string, ...any) pgx.Row { return nilRow{} }

type nilRow struct{}

func (nilRow) Scan(...any) error { return pgx.ErrNoRows }

// Turn red by having CreateUser swallow an Exec error: the last insert then
// fails silently and the caller commits a half-built principal.
func TestCreateUserStopsAtFirstFailedInsert(t *testing.T) {
	db := &failAtExec{failOn: 5}
	_, err := CreateUser(context.Background(), db, CreateUserInput{
		DisplayName: "op", Org: "acme", Team: "platform", Machine: "laptop",
	})
	if !errors.Is(err, errInjected) {
		t.Fatalf("CreateUser err = %v, want the injected failure wrapped", err)
	}
	if len(db.seen) != 5 {
		t.Fatalf("CreateUser issued %d writes, want it to stop at the 5th: %v", len(db.seen), db.seen)
	}
	if !strings.Contains(db.seen[4], "api_token") {
		t.Fatalf("5th write was %q, want the api_token insert", db.seen[4])
	}
}

// Turn red by giving the api_token insert a non-null expires_at: user tokens
// must not expire, because lookup.go only TTL-caps agent tokens.
func TestCreateUserTokenHasNoExpiry(t *testing.T) {
	db := &failAtExec{failOn: 0}
	res, err := CreateUser(context.Background(), db, CreateUserInput{
		DisplayName: "op", Org: "acme", Team: "platform", Machine: "laptop",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if res.Token == "" {
		t.Fatal("CreateUser returned an empty token")
	}
	if !strings.Contains(db.seen[4], "expires_at") {
		return
	}
	t.Fatalf("api_token insert names expires_at: %q", db.seen[4])
}
