package domain

import (
	"errors"
	"time"
)

var (
	ErrNotFound             = errors.New("заявка не найдена")
	ErrForbidden            = errors.New("заявка принадлежит другому клиенту")
	ErrConflict             = errors.New("состояние заявки изменилось")
	ErrInvalidKycResponse   = errors.New("application kyc id and kyc id mismatched")
	ErrApplicationCompleted = errors.New("application already completed")
	ErrKycNotReady          = errors.New("kyc in progress")
	ErrKycRejected          = errors.New("kyc rejected")
	ErrInvalidInput         = errors.New("invalid input")
	ErrFailedToCreateTx     = errors.New("failed to create tx")
	ErrFailedToCommitTx     = errors.New("failed to commit tx")
)

type ApplicationStatus string

const (
	Initial              ApplicationStatus = "INITIAL"
	KycPending           ApplicationStatus = "KYC_PENDING"
	KycCompleted         ApplicationStatus = "KYC_COMPLETED"
	ApplicationCompleted ApplicationStatus = "APPLICATION_COMPLETED"
)

type Application struct {
	ID           string
	ClientID     string
	Status       ApplicationStatus
	KycSessionID string
	Version      int64
}

type KycStatus string

const (
	KycInProgress KycStatus = "in_progress"
	KycApproved   KycStatus = "approved"
	KycRejected   KycStatus = "rejected"
)

type KycResult struct {
	SessionID string
	Status    KycStatus
}

type SMSStatus string

const (
	SMSAccepted SMSStatus = "accepted"
	SMSRejected SMSStatus = "rejected"
	SMSUnknown  SMSStatus = "unknown"
)

type SMSRequest struct {
	NotificationID string
	ClientID       string
	Text           string
}

type SMSResult struct {
	Status      SMSStatus
	ProviderRef string
}

type NotificationState string

const (
	NotificationPending NotificationState = "PENDING"
	NotificationSent    NotificationState = "SENT"
	NotificationFailed  NotificationState = "FAILED"
	NotificationManual  NotificationState = "MANUAL"
)

type Notification struct {
	ID            string
	ApplicationID string
	ClientID      string
	Text          string

	State         NotificationState
	Attempts      int
	NextAttemptAt time.Time
	LeaseToken    string
	LeaseUntil    time.Time
	ProviderRef   string
	LastError     string
	CreatedAt     time.Time
}
