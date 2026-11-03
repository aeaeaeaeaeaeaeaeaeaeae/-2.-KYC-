// Package fakes содержит учебные реализации внешних клиентов (KYC и SMS)
// с предсказуемым поведением, чтобы локально воспроизвести сценарии из задания.
package fakes

import (
	"context"
	"errors"
	"strings"
	"time"

	"kyc/internal/domain"
)

// KycClient решает исход по ID сессии:
//
//	sess-approved-*  -> approved
//	sess-slow        -> approved через 2 секунды (для двух одновременных вызовов)
//	sess-in-progress -> in_progress
//	sess-rejected    -> rejected
//	sess-mismatch    -> ответ про другую сессию
//	sess-garbage     -> неизвестный статус
//	sess-timeout     -> висит до отмены контекста
//	остальное        -> ошибка "сессия не найдена"
type KycClient struct{}

func (KycClient) FetchStatus(ctx context.Context, sessionID string) (domain.KycResult, error) {
	switch {
	case strings.HasPrefix(sessionID, "sess-approved-"):
		return domain.KycResult{SessionID: sessionID, Status: domain.KycApproved}, nil
	case sessionID == "sess-slow":
		select {
		case <-time.After(2 * time.Second):
			return domain.KycResult{SessionID: sessionID, Status: domain.KycApproved}, nil
		case <-ctx.Done():
			return domain.KycResult{}, ctx.Err()
		}
	case sessionID == "sess-in-progress":
		return domain.KycResult{SessionID: sessionID, Status: domain.KycInProgress}, nil
	case sessionID == "sess-rejected":
		return domain.KycResult{SessionID: sessionID, Status: domain.KycRejected}, nil
	case sessionID == "sess-mismatch":
		return domain.KycResult{SessionID: "sess-other", Status: domain.KycApproved}, nil
	case sessionID == "sess-garbage":
		return domain.KycResult{SessionID: sessionID, Status: "weird"}, nil
	case sessionID == "sess-timeout":
		<-ctx.Done()
		return domain.KycResult{}, ctx.Err()
	default:
		return domain.KycResult{}, errors.New("fake kyc: session not found")
	}
}
