package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"kyc/internal/domain"
	"time"
)

type Repository struct {
	db        *sql.DB
	dbTimeout time.Duration
}

func NewRepository(db *sql.DB, timeout time.Duration) *Repository {
	if timeout <= 0 {
		timeout = time.Second * 3
	}
	return &Repository{
		db:        db,
		dbTimeout: timeout,
	}
}

func (r *Repository) FindByID(ctx context.Context, applicationId string) (domain.Application, error) {
	const query = `
	SELECT id, client_id, status, kyc_session_id, version FROM application WHERE id = $1
	`

	ctx, cancel := context.WithTimeout(ctx, r.dbTimeout)
	defer cancel()

	var application domain.Application
	if err := r.db.QueryRowContext(ctx, query, applicationId).Scan(
		&application.ID, &application.ClientID, &application.Status, &application.KycSessionID, &application.Version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Application{}, fmt.Errorf("application with id %q, was not found %w", applicationId, domain.ErrNotFound)
		}
		return domain.Application{}, err
	}

	return application, nil
}

func (r *Repository) UpdateApplicationAndSaveNotification(ctx context.Context, application domain.Application, notification domain.Notification) error {
	ctx, cancel := context.WithTimeout(ctx, r.dbTimeout)
	defer cancel()

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("%w: %w", domain.ErrFailedToCreateTx, err)
	}
	defer tx.Rollback()

	const application_query = `UPDATE application SET status = 'KYC_COMPLETED', version = version + 1 WHERE id = $1 AND status = 'KYC_PENDING' AND kyc_session_id = $2 AND version = $3`

	const notification_query = `INSERT INTO notifications (id, application_id, client_id, text) VALUES ($1, $2, $3, $4)`

	res, err := tx.ExecContext(ctx, application_query, application.ID, application.KycSessionID, application.Version)
	if err != nil {
		return fmt.Errorf("error while update application: %w", err)
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("error while rows affected: %w", err)
	}

	if affected == 0 {
		return domain.ErrConflict
	}

	res, err = tx.ExecContext(ctx, notification_query, notification.ID, notification.ApplicationID, notification.ClientID, notification.Text)
	if err != nil {
		return fmt.Errorf("error while insert notification: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%w: %w", domain.ErrFailedToCommitTx, err)
	}

	return nil
}
