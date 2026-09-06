package memory

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/agentic-substrate/substrate/internal/identity"
	"github.com/agentic-substrate/substrate/internal/store"
)

// BatchItem is one outbox payload: a memory.write body plus the client_id
// that makes the drain idempotent (EDD R7/R17, Gotcha 10).
type BatchItem struct {
	ClientID string `json:"client_id"`
	WriteIn
}

// BatchResult is one drain outcome. Duplicate is true when client_id was
// already receipted; SubjectID is the original memory in that case.
type BatchResult struct {
	ClientID  string `json:"client_id"`
	SubjectID string `json:"subject_id"`
	Duplicate bool   `json:"duplicate"`
}

// Batch writes each payload in its own transaction. The ingest_receipt row
// and the memory row are created together; a known client_id writes nothing
// and returns the original subject_id. Splitting those two inserts across
// transactions is how a crash poisons the store with corroborating duplicates.
func (s *Service) Batch(ctx context.Context, items []BatchItem) ([]BatchResult, error) {
	p := identity.FromContext(ctx)
	if p == nil {
		return nil, store.ErrNoPrincipal
	}
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	out := make([]BatchResult, 0, len(items))
	for _, item := range items {
		res, err := s.batchOne(ctx, st, p, item)
		if err != nil {
			return out, err
		}
		out = append(out, res)
	}
	return out, nil
}

func (s *Service) batchOne(ctx context.Context, st *store.Store, p *identity.Principal, item BatchItem) (BatchResult, error) {
	cid, err := uuid.Parse(item.ClientID)
	if err != nil {
		return BatchResult{}, fmt.Errorf("memory.batch: client_id: %w", err)
	}
	prep, err := prepareWrite(ctx, item.WriteIn)
	if err != nil {
		return BatchResult{}, err
	}

	var res BatchResult
	res.ClientID = cid.String()
	err = st.TxChecked(ctx, "memory.write", prep.sc, func(tx pgx.Tx) error {
		q := store.New(tx)
		existing, err := q.GetIngestReceipt(ctx, pgUUID(cid))
		if err == nil {
			res.Duplicate = true
			res.SubjectID = uuid.UUID(existing.SubjectID.Bytes).String()
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("memory.batch: receipt: %w", err)
		}

		id, err := uuid.NewV7()
		if err != nil {
			return fmt.Errorf("memory.batch: %w", err)
		}
		if err := q.InsertIngestReceipt(ctx, store.InsertIngestReceiptParams{
			ClientID:    pgUUID(cid),
			PrincipalID: pgUUID(p.ID),
			SubjectType: "memory",
			SubjectID:   pgUUID(id),
		}); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				existing, rerr := q.GetIngestReceipt(ctx, pgUUID(cid))
				if rerr != nil {
					return fmt.Errorf("memory.batch: receipt race: %w", rerr)
				}
				res.Duplicate = true
				res.SubjectID = uuid.UUID(existing.SubjectID.Bytes).String()
				return nil
			}
			return fmt.Errorf("memory.batch: receipt: %w", err)
		}
		if s.FailAfterReceipt != nil {
			if err := s.FailAfterReceipt(); err != nil {
				return err
			}
		}
		if _, err := prep.insert(ctx, tx, id, p.ID); err != nil {
			return err
		}
		res.SubjectID = id.String()
		return nil
	})
	if err != nil {
		return BatchResult{}, err
	}
	if !res.Duplicate {
		_ = s.storeEmbedding(ctx, st, res.SubjectID, prep.title, prep.body)
	}
	return res, nil
}
