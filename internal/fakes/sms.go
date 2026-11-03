package fakes

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"

	"github.com/google/uuid"

	"kyc/internal/domain"
)

// SMSClient — учебный провайдер. Поведение выбирается по последней цифре ClientID:
//
//	...1 -> принимает сразу
//	...2 -> первые 2 вызова — ошибка связи, потом принимает
//	...3 -> окончательный отказ (rejected)
//	...4 -> всегда unknown
//	...5 -> принимает SMS, но ответ "теряется" (таймаут); следующий вызов с тем же ID
//	        возвращает тот же результат без повторной отправки
//
// Как и договорено в задании, повтор того же NotificationID не создаёт второе SMS.
// Состояние живёт в памяти процесса.
type SMSClient struct {
	mu       sync.Mutex
	calls    map[string]int
	accepted map[string]domain.SMSResult
}

func NewSMSClient() *SMSClient {
	return &SMSClient{calls: map[string]int{}, accepted: map[string]domain.SMSResult{}}
}

func (c *SMSClient) SendSMS(ctx context.Context, req domain.SMSRequest) (domain.SMSResult, error) {
	c.mu.Lock()
	c.calls[req.NotificationID]++
	call := c.calls[req.NotificationID]
	if res, ok := c.accepted[req.NotificationID]; ok {
		c.mu.Unlock()
		slog.Info("fake sms: duplicate request, no new sms sent", "notification_id", req.NotificationID, "call", call)
		return res, nil
	}
	c.mu.Unlock()

	switch {
	case strings.HasSuffix(req.ClientID, "2") && call <= 2:
		slog.Info("fake sms: connection error", "notification_id", req.NotificationID, "call", call)
		return domain.SMSResult{}, errors.New("fake sms: connection reset")
	case strings.HasSuffix(req.ClientID, "3"):
		slog.Info("fake sms: rejected", "notification_id", req.NotificationID, "call", call)
		return domain.SMSResult{Status: domain.SMSRejected}, nil
	case strings.HasSuffix(req.ClientID, "4"):
		slog.Info("fake sms: unknown", "notification_id", req.NotificationID, "call", call)
		return domain.SMSResult{Status: domain.SMSUnknown}, nil
	case strings.HasSuffix(req.ClientID, "5") && call == 1:
		c.accept(req.NotificationID)
		slog.Info("fake sms: accepted, but response lost", "notification_id", req.NotificationID, "call", call)
		<-ctx.Done()
		return domain.SMSResult{}, ctx.Err()
	default:
		res := c.accept(req.NotificationID)
		slog.Info("fake sms: accepted", "notification_id", req.NotificationID, "call", call, "provider_ref", res.ProviderRef)
		return res, nil
	}
}

func (c *SMSClient) LookupSMS(_ context.Context, notificationID string) (domain.SMSResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if res, ok := c.accepted[notificationID]; ok {
		return res, nil
	}
	return domain.SMSResult{Status: domain.SMSUnknown}, nil
}

func (c *SMSClient) accept(notificationID string) domain.SMSResult {
	c.mu.Lock()
	defer c.mu.Unlock()
	if res, ok := c.accepted[notificationID]; ok {
		return res
	}
	res := domain.SMSResult{Status: domain.SMSAccepted, ProviderRef: "prov-" + uuid.NewString()[:8]}
	c.accepted[notificationID] = res
	return res
}
