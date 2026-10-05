package postgres

import (
	"context"
	"musicgetter/internal/domain"
)

type BotQueries struct{ db DBTX }

func NewBotQueries(db DBTX) *BotQueries { return &BotQueries{db} }
func (q *BotQueries) Imports(ctx context.Context, owner domain.ID, cursor string) ([]domain.ImportSummary, error) {
	rows, err := q.db.Query(ctx, `SELECT id,source,state FROM musicgetter.imports WHERE owner_id=$1 AND ($2='' OR (created_at,id)<(SELECT created_at,id FROM musicgetter.imports WHERE owner_id=$1 AND id=NULLIF($2,'')::uuid)) ORDER BY created_at DESC,id DESC LIMIT 10`, owner, cursor)
	if err != nil {
		return nil, repositoryError(err)
	}
	defer rows.Close()
	result := []domain.ImportSummary{}
	for rows.Next() {
		var v domain.ImportSummary
		if err = rows.Scan(&v.ID, &v.Source, &v.State); err != nil {
			return nil, repositoryError(err)
		}
		result = append(result, v)
	}
	return result, repositoryError(rows.Err())
}
func (q *BotQueries) Status(ctx context.Context, owner domain.ID, id string) (domain.ImportSummary, error) {
	var v domain.ImportSummary
	err := q.db.QueryRow(ctx, `SELECT i.id,i.source,i.state,count(t.id),count(t.id) FILTER(WHERE t.state IN ('added','already_present')),count(t.id) FILTER(WHERE t.state='failed'),count(t.id) FILTER(WHERE t.state='needs_review') FROM (SELECT id,source,state FROM musicgetter.imports WHERE owner_id=$1 AND ($2='' OR id=NULLIF($2,'')::uuid) ORDER BY created_at DESC,id DESC LIMIT 1) i LEFT JOIN musicgetter.import_items t ON t.import_id=i.id AND t.owner_id=$1 GROUP BY i.id,i.source,i.state`, owner, id).Scan(&v.ID, &v.Source, &v.State, &v.Total, &v.Done, &v.Failed, &v.NeedsReview)
	return v, repositoryError(err)
}
func (q *BotQueries) Playlists(ctx context.Context, owner domain.ID, cursor string) ([]domain.CollectionSummary, error) {
	rows, err := q.db.Query(ctx, `SELECT id,title,kind FROM musicgetter.destination_collections WHERE owner_id=$1 AND ($2='' OR id>NULLIF($2,'')::uuid) ORDER BY id LIMIT 10`, owner, cursor)
	if err != nil {
		return nil, repositoryError(err)
	}
	defer rows.Close()
	result := []domain.CollectionSummary{}
	for rows.Next() {
		var v domain.CollectionSummary
		if err = rows.Scan(&v.ID, &v.Title, &v.Kind); err != nil {
			return nil, repositoryError(err)
		}
		result = append(result, v)
	}
	return result, repositoryError(rows.Err())
}
func (q *BotQueries) Sessions(ctx context.Context, owner domain.ID) (int, error) {
	var n int
	err := q.db.QueryRow(ctx, `SELECT count(*) FROM musicgetter.extension_sessions WHERE owner_id=$1 AND revoked_at IS NULL AND expires_at>statement_timestamp()`, owner).Scan(&n)
	return n, repositoryError(err)
}

// Claim before side effects: restart/repeated delivery never executes a command twice.
// Telegram has no idempotency key for sendMessage; a crash can lose a reply.
func (q *BotQueries) ClaimUpdate(ctx context.Context, botID, updateID int64) (bool, error) {
	result, err := q.db.Exec(ctx, `INSERT INTO musicgetter.telegram_updates(bot_id,update_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, botID, updateID)
	if err != nil {
		return false, repositoryError(err)
	}
	return result.RowsAffected() == 1, nil
}
