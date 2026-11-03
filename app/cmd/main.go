package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"kyc/internal/domain"
	"kyc/internal/fakes"
	"kyc/internal/repository"
	"kyc/internal/service"
	"kyc/internal/worker"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))

	dsn := envOr("DATABASE_URL", "postgres://kyc:kyc@localhost:5432/kyc?sslmode=disable")
	addr := envOr("HTTP_ADDR", ":8080")

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		slog.Error("open db", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := waitForDB(db, 30*time.Second); err != nil {
		slog.Error("db is not reachable", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	svc := service.NewService(repository.NewRepository(db, 3*time.Second), fakes.KycClient{}, 3*time.Second)
	appRepo := repository.NewRepository(db, 3*time.Second)
	w := worker.NewWorker(*worker.NewWorkerRepo(db, 3*time.Second), fakes.NewSMSClient())

	workersDone := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(workersDone)
	}()

	mux := http.NewServeMux()

	// Завершить KYC. ClientID берётся из заголовка: так эмулируется "проверенный контекст пользователя".
	mux.HandleFunc("POST /applications/{id}/complete-kyc", func(rw http.ResponseWriter, r *http.Request) {
		app, err := svc.CompleteKYC(r.Context(), r.PathValue("id"), r.Header.Get("X-Client-ID"))
		if err != nil {
			writeError(rw, err)
			return
		}
		writeJSON(rw, http.StatusOK, toJSON(app))
	})

	// Отладочные эндпоинты, чтобы посмотреть состояние.
	mux.HandleFunc("GET /applications/{id}", func(rw http.ResponseWriter, r *http.Request) {
		app, err := appRepo.FindByID(r.Context(), r.PathValue("id"))
		if err != nil {
			writeError(rw, err)
			return
		}
		writeJSON(rw, http.StatusOK, toJSON(app))
	})
	mux.HandleFunc("GET /notifications", func(rw http.ResponseWriter, r *http.Request) {
		list, err := listNotifications(r.Context(), db)
		if err != nil {
			writeError(rw, err)
			return
		}
		writeJSON(rw, http.StatusOK, list)
	})

	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		slog.Info("http server started", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("http shutdown", "error", err)
	}

	select {
	case <-workersDone:
		slog.Info("workers stopped")
	case <-shutdownCtx.Done():
		slog.Warn("workers did not stop in time; claimed notifications will be picked up after lease expiry")
	}
}

type applicationJSON struct {
	ID           string `json:"id"`
	ClientID     string `json:"client_id"`
	Status       string `json:"status"`
	KycSessionID string `json:"kyc_session_id"`
	Version      int64  `json:"version"`
}

func toJSON(a domain.Application) applicationJSON {
	return applicationJSON{ID: a.ID, ClientID: a.ClientID, Status: string(a.Status), KycSessionID: a.KycSessionID, Version: a.Version}
}

func writeError(rw http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, domain.ErrInvalidInput):
		status = http.StatusBadRequest
	case errors.Is(err, domain.ErrNotFound):
		status = http.StatusNotFound
	case errors.Is(err, domain.ErrForbidden):
		status = http.StatusForbidden
	case errors.Is(err, domain.ErrConflict), errors.Is(err, domain.ErrApplicationCompleted), errors.Is(err, domain.ErrKycNotReady):
		status = http.StatusConflict
	case errors.Is(err, domain.ErrKycRejected):
		status = http.StatusUnprocessableEntity
	case errors.Is(err, domain.ErrInvalidKycResponse):
		status = http.StatusBadGateway
	case errors.Is(err, context.DeadlineExceeded):
		status = http.StatusGatewayTimeout
	}
	if status == http.StatusInternalServerError {
		slog.Error("request failed", "error", err)
	}
	writeJSON(rw, status, map[string]string{"error": err.Error()})
}

func writeJSON(rw http.ResponseWriter, status int, v any) {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(status)
	enc := json.NewEncoder(rw)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

type notificationJSON struct {
	ID            string     `json:"id"`
	ApplicationID string     `json:"application_id"`
	ClientID      string     `json:"client_id"`
	State         string     `json:"state"`
	Attempts      int        `json:"attempts"`
	NextAttemptAt time.Time  `json:"next_attempt_at"`
	LeaseUntil    *time.Time `json:"lease_until,omitempty"`
	ProviderRef   *string    `json:"provider_ref,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

// Текст SMS сюда не выводится намеренно.
func listNotifications(ctx context.Context, db *sql.DB) ([]notificationJSON, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, application_id, client_id, state, attempts, next_attempt_at, lease_until, provider_ref, created_at
		FROM notifications ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []notificationJSON{}
	for rows.Next() {
		var n notificationJSON
		var leaseUntil sql.NullTime
		var providerRef sql.NullString
		if err := rows.Scan(&n.ID, &n.ApplicationID, &n.ClientID, &n.State, &n.Attempts, &n.NextAttemptAt, &leaseUntil, &providerRef, &n.CreatedAt); err != nil {
			return nil, err
		}
		if leaseUntil.Valid {
			n.LeaseUntil = &leaseUntil.Time
		}
		if providerRef.Valid && providerRef.String != "" {
			n.ProviderRef = &providerRef.String
		}
		list = append(list, n)
	}
	return list, rows.Err()
}

func waitForDB(db *sql.DB, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := db.PingContext(ctx)
		cancel()
		if err == nil || time.Now().After(deadline) {
			return err
		}
		time.Sleep(time.Second)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
