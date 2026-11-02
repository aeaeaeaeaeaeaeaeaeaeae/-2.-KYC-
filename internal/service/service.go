package service

import (
	"context"
	"errors"
	"fmt"
	"kyc/internal/domain"
	"time"

	"github.com/google/uuid"
)

type ApplicationRepository interface {
	FindByID(context.Context, string) (domain.Application, error)
	UpdateApplicationAndSaveNotification(ctx context.Context, application domain.Application, notification domain.Notification) error
}

type KycClient interface {
	FetchStatus(context.Context, string) (domain.KycResult, error)
}

type Service struct {
	Applications  ApplicationRepository
	Kyc           KycClient
	clientTimeout time.Duration
}

func NewService(applications ApplicationRepository, kyc KycClient, clientTimeout time.Duration) *Service {
	if clientTimeout <= 0 {
		clientTimeout = time.Second * 3
	}

	return &Service{
		Applications:  applications,
		Kyc:           kyc,
		clientTimeout: clientTimeout,
	}
}

func (s *Service) CompleteKYC(ctx context.Context, applicationID, clientID string) (domain.Application, error) {

	if _, err := uuid.Parse(applicationID); err != nil {
		return domain.Application{}, fmt.Errorf("%w: application id %q: %v", domain.ErrInvalidInput, applicationID, err)
	}

	application, err := s.Applications.FindByID(ctx, applicationID)
	if err != nil {
		return domain.Application{}, err
	}

	if application.ClientID != clientID {
		return domain.Application{}, domain.ErrForbidden
	}

	if application.Status == domain.ApplicationCompleted {
		return domain.Application{}, domain.ErrApplicationCompleted
	}

	if application.Status == domain.KycCompleted {
		return application, nil
	}

	if application.Status != domain.KycPending {
		return domain.Application{}, domain.ErrConflict
	}

	if application.KycSessionID == "" {
		return domain.Application{}, domain.ErrConflict
	}

	kycCtx, cancel := context.WithTimeout(ctx, s.clientTimeout)
	kycResult, err := s.Kyc.FetchStatus(kycCtx, application.KycSessionID)
	cancel()
	if err != nil {
		return domain.Application{}, err
	}

	if application.KycSessionID != kycResult.SessionID {
		return domain.Application{}, domain.ErrInvalidKycResponse
	}

	if kycResult.Status == domain.KycApproved {

		notification := domain.Notification{
			ID:            uuid.NewString(),
			ApplicationID: application.ID,
			ClientID:      application.ClientID,
			Text:          "kyc completed",
		}

		err = s.Applications.UpdateApplicationAndSaveNotification(ctx, application, notification)
		if errors.Is(err, domain.ErrConflict) {
			current, findErrer := s.Applications.FindByID(ctx, applicationID)
			if findErrer != nil {
				return domain.Application{}, findErrer
			}

			if current.Status == domain.KycCompleted && application.KycSessionID == current.KycSessionID {
				return current, nil
			}
		}

		if err != nil {
			return domain.Application{}, err
		}

		application.Status = domain.KycCompleted
		application.Version++
		return application, nil
	} else if kycResult.Status == domain.KycInProgress {
		return domain.Application{}, domain.ErrKycNotReady
	} else if kycResult.Status == domain.KycRejected {
		return domain.Application{}, domain.ErrKycRejected
	}

	return domain.Application{}, domain.ErrInvalidKycResponse
}
