package worker

import (
	"context"
	"database/sql"
	"kyc/internal/domain"
	"time"

	"github.com/google/uuid"
)

type WorkerRepo struct {
	db        *sql.DB
	dbTimeout time.Duration
}

func NewWorkerRepo(db *sql.DB, timeout time.Duration) *WorkerRepo {
	if timeout <= 0 {
		timeout = time.Second * 3
	}
	return &WorkerRepo{
		db:        db,
		dbTimeout: timeout,
	}
}

func (r *WorkerRepo) WorkerLock(ctx context.Context, limit int) []domain.Notification {
	ctx, cancel := context.WithTimeout(ctx, r.dbTimeout)
	defer cancel()

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil
	}
	defer tx.Rollback()
	leaseToken := uuid.NewString()

	const blocking_query = `SELECT id FROM notifications WHERE (state = 'PENDING' AND next_attempt_at <= now()) OR (state = 'IN_FLIGHT' AND lease_until < now()) ORDER BY next_attempt_at FOR UPDATE SKIP LOCKED LIMIT $1`
	const update_query = `UPDATE notifications SET state = 'IN_FLIGHT', lease_token = $1, lease_until = now() + make_interval(secs => 30), attempts = attempts + 1 WHERE id = ANY($2) 
						  RETURNING id, client_id, text, attempts, created_at, lease_token`

	blockingRows, err := tx.QueryContext(ctx, blocking_query, limit)
	if err != nil {
		return nil
	}
	defer blockingRows.Close()

	var notificationsId []string
	for blockingRows.Next() {
		var id string
		if err = blockingRows.Scan(&id); err != nil {
			continue
		}
		notificationsId = append(notificationsId, id)
	}

	if blockingRows.Err() != nil {
		return nil
	}

	if len(notificationsId) == 0 {
		tx.Commit()
		return nil
	}

	updateRows, err := tx.QueryContext(ctx, update_query, leaseToken, notificationsId)
	if err != nil {
		return nil
	}
	defer updateRows.Close()

	var notifications []domain.Notification
	for updateRows.Next() {
		var notification domain.Notification
		if err = updateRows.Scan(&notification.ID, &notification.ClientID, &notification.Text, &notification.Attempts, &notification.CreatedAt, &notification.LeaseToken); err != nil {
			continue
		}
		notifications = append(notifications, notification)
	}

	if updateRows.Err() != nil {
		return nil
	}

	if err = tx.Commit(); err != nil {
		return nil
	}

	return notifications
}

func (r *WorkerRepo) SaveStatus(ctx context.Context, id, leaseToken string, state domain.NotificationState, nextAttemptAt *time.Time, providerResponse string) bool {
	const query = `UPDATE notifications SET next_attempt_at = COALESCE($4, next_attempt_at), provider_ref = $5, state = $1, lease_token = NULL, lease_until = NULL
                   WHERE id = $2 AND lease_token = $3 AND state = 'IN_FLIGHT'`

	ctx, cancel := context.WithTimeout(ctx, r.dbTimeout)
	defer cancel()

	res, err := r.db.ExecContext(ctx, query, state, id, leaseToken, nextAttemptAt, providerResponse)
	if err != nil {
		return false
	}

	rowsAffected, _ := res.RowsAffected()

	return rowsAffected == 1

}
