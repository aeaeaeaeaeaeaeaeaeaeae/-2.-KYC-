package worker

import (
	"context"
	"kyc/internal/domain"
	"log/slog"
	"sync"
	"time"
)

type NotificationClient interface {
	SendSMS(context.Context, domain.SMSRequest) (domain.SMSResult, error)
	LookupSMS(context.Context, string) (domain.SMSResult, error)
}

type Worker struct {
	WorkerRepo         WorkerRepo
	NotificationClient NotificationClient
	wg                 sync.WaitGroup
}

func NewWorker(workerRepo WorkerRepo, notificationClient NotificationClient) *Worker {
	return &Worker{
		WorkerRepo:         workerRepo,
		NotificationClient: notificationClient,
	}
}

func (w *Worker) taskWorker(ctx context.Context) {
	notifications, err := w.WorkerRepo.WorkerLock(ctx, 10)
	if err != nil {
		if ctx.Err() == nil {
			slog.Error("claim notifications", "error", err)
		}
		return
	}

	for _, v := range notifications {
		smcContext, cancel := context.WithTimeout(ctx, time.Second*2)
		smsResult, err := w.NotificationClient.SendSMS(smcContext, domain.SMSRequest{
			NotificationID: v.ID,
			ClientID:       v.ClientID,
			Text:           v.Text,
		})
		cancel()

		if err != nil {
			slog.Warn("send sms failed", "notification_id", v.ID, "attempt", v.Attempts, "error", err)
		} else if smsResult.Status != domain.SMSAccepted && smsResult.Status != domain.SMSRejected {
			slog.Warn("sms result unknown", "notification_id", v.ID, "attempt", v.Attempts, "status", smsResult.Status)
		}

		var state domain.NotificationState
		var nextAttempt *time.Time

		switch {
		case err == nil && smsResult.Status == domain.SMSAccepted:
			state = domain.NotificationSent
		case err == nil && smsResult.Status == domain.SMSRejected:
			state = domain.NotificationFailed
		case v.Attempts >= 10:
			state = domain.NotificationManual
		default:
			state = domain.NotificationPending
			nextAttempt = new(time.Now().Add(10 * time.Second << (v.Attempts - 1)))
		}
		saved, err := w.WorkerRepo.SaveStatus(ctx, v.ID, v.LeaseToken, state, nextAttempt, smsResult.ProviderRef)
		switch {
		case err != nil:
			slog.Error("save notification result", "notification_id", v.ID, "state", state, "error", err)
		case !saved:
			slog.Warn("notification lease lost, result discarded", "notification_id", v.ID, "state", state)
		case state == domain.NotificationFailed || state == domain.NotificationManual:
			slog.Warn("notification needs manual review", "notification_id", v.ID, "state", state, "attempts", v.Attempts)
		}
	}
}

func (w *Worker) Run(ctx context.Context) {

	w.wg.Add(10)
	for range 10 {
		go func() {
			defer w.wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Second * 3):
				}

				w.taskWorker(ctx)
			}
		}()
	}

	w.wg.Wait()
}
