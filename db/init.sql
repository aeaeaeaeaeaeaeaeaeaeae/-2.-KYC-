-- Схема

CREATE TABLE application (
    id             uuid        PRIMARY KEY,
    client_id      uuid        NOT NULL,
    status         text        NOT NULL,
    kyc_session_id text        NOT NULL DEFAULT '',
    version        bigint      NOT NULL DEFAULT 1
);

CREATE TABLE notifications (
    id              uuid        PRIMARY KEY,
    application_id  uuid        NOT NULL REFERENCES application (id),
    client_id       uuid        NOT NULL,
    text            text        NOT NULL,
    state           text        NOT NULL DEFAULT 'PENDING',
    attempts        int         NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    lease_token     uuid,
    lease_until     timestamptz,
    provider_ref    text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (application_id)
);

CREATE INDEX notifications_ready_idx
    ON notifications (next_attempt_at)
    WHERE state IN ('PENDING', 'IN_FLIGHT');

-- Тестовые данные.
-- Последняя цифра client_id выбирает поведение учебного SMS-провайдера (см. internal/fakes/sms.go),
-- kyc_session_id — поведение учебного KYC (см. internal/fakes/kyc.go).

INSERT INTO application (id, client_id, status, kyc_session_id) VALUES
    -- успешное завершение KYC, разные исходы SMS
    ('a0000000-0000-0000-0000-000000000001', 'c0000000-0000-0000-0000-000000000001', 'KYC_PENDING', 'sess-approved-01'),  -- SMS принято
    ('a0000000-0000-0000-0000-000000000002', 'c0000000-0000-0000-0000-000000000002', 'KYC_PENDING', 'sess-approved-02'),  -- SMS: 2 ошибки связи, потом принято
    ('a0000000-0000-0000-0000-000000000003', 'c0000000-0000-0000-0000-000000000003', 'KYC_PENDING', 'sess-approved-03'),  -- SMS: окончательный отказ -> FAILED
    ('a0000000-0000-0000-0000-000000000004', 'c0000000-0000-0000-0000-000000000004', 'KYC_PENDING', 'sess-approved-04'),  -- SMS: всегда unknown -> повторы -> MANUAL
    ('a0000000-0000-0000-0000-000000000005', 'c0000000-0000-0000-0000-000000000005', 'KYC_PENDING', 'sess-approved-05'),  -- SMS принято, но ответ потерян
    -- два одновременных вызова (KYC отвечает 2 секунды)
    ('a0000000-0000-0000-0000-000000000006', 'c0000000-0000-0000-0000-000000000001', 'KYC_PENDING', 'sess-slow'),
    -- неуспешные сценарии KYC
    ('a0000000-0000-0000-0000-000000000007', 'c0000000-0000-0000-0000-000000000001', 'KYC_PENDING', 'sess-in-progress'),
    ('a0000000-0000-0000-0000-000000000008', 'c0000000-0000-0000-0000-000000000001', 'KYC_PENDING', 'sess-rejected'),
    ('a0000000-0000-0000-0000-000000000009', 'c0000000-0000-0000-0000-000000000001', 'KYC_PENDING', 'sess-mismatch'),
    ('a0000000-0000-0000-0000-000000000010', 'c0000000-0000-0000-0000-000000000001', 'KYC_PENDING', 'sess-garbage'),
    ('a0000000-0000-0000-0000-000000000011', 'c0000000-0000-0000-0000-000000000001', 'KYC_PENDING', 'sess-timeout'),
    -- неподходящее состояние заявки
    ('a0000000-0000-0000-0000-000000000012', 'c0000000-0000-0000-0000-000000000001', 'INITIAL', ''),
    ('a0000000-0000-0000-0000-000000000013', 'c0000000-0000-0000-0000-000000000001', 'APPLICATION_COMPLETED', 'sess-approved-13'),
    ('a0000000-0000-0000-0000-000000000014', 'c0000000-0000-0000-0000-000000000001', 'KYC_PENDING', '');
