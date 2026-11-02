package worker

import (
	"context"
	"kyc/internal/domain"
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
	notifications := w.WorkerRepo.WorkerLock(ctx, 10)

	for _, v := range notifications {
		smcContext, cancel := context.WithTimeout(ctx, time.Second*2)
		smsResult, err := w.NotificationClient.SendSMS(smcContext, domain.SMSRequest{
			NotificationID: v.ID,
			ClientID:       v.ClientID,
			Text:           v.Text,
		})
		cancel()

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
		w.WorkerRepo.SaveStatus(ctx, v.ID, v.LeaseToken, domain.NotificationState(state), nextAttempt, smsResult.ProviderRef)
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
