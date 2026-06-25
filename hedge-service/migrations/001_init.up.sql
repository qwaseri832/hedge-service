-- 001_init.up.sql

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Статусы переводов
CREATE TYPE transfer_status AS ENUM ('pending', 'processing', 'processed', 'failed');

-- Статусы ордеров
CREATE TYPE order_status AS ENUM ('pending', 'placed', 'partially_filled', 'filled', 'failed', 'cancelled');

-- Статусы уведомлений
CREATE TYPE notification_status AS ENUM ('pending', 'sent', 'failed');

-- Notification channels
CREATE TYPE notification_channel AS ENUM ('email', 'webhook');

-- Входящие переводы
CREATE TABLE transfers (
    id           UUID         PRIMARY KEY DEFAULT uuid_generate_v4(),
    external_id  TEXT         NOT NULL,                -- идемпотентный ключ от платёжной системы
    client_id    TEXT         NOT NULL,
    amount_fiat  NUMERIC(20,8) NOT NULL,
    currency     CHAR(3)      NOT NULL,                -- USD, RUB
    wallet_addr  TEXT         NOT NULL,                -- крипто-кошелёк клиента
    status       transfer_status NOT NULL DEFAULT 'pending',
    retry_count  INT          NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    CONSTRAINT transfers_external_id_unique UNIQUE (external_id)  -- гарантия идемпотентности
);

CREATE INDEX idx_transfers_status     ON transfers (status) WHERE status IN ('pending', 'processing');
CREATE INDEX idx_transfers_client_id  ON transfers (client_id);
CREATE INDEX idx_transfers_created_at ON transfers (created_at DESC);

-- Ордера на бирже
CREATE TABLE orders (
    id                UUID          PRIMARY KEY DEFAULT uuid_generate_v4(),
    transfer_id       UUID          NOT NULL REFERENCES transfers(id),
    client_order_id   TEXT          NOT NULL,           -- наш идемпотентный ключ для биржи
    exchange_order_id TEXT          NOT NULL DEFAULT '',-- ID от биржи
    symbol            TEXT          NOT NULL,           -- BTCUSDT
    amount_fiat       NUMERIC(20,8) NOT NULL,
    amount_crypto     NUMERIC(20,8) NOT NULL DEFAULT 0,
    price             NUMERIC(20,8) NOT NULL DEFAULT 0,
    status            order_status  NOT NULL DEFAULT 'pending',
    retry_count       INT           NOT NULL DEFAULT 0,
    fail_reason       TEXT          NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ   NOT NULL DEFAULT NOW(),

    CONSTRAINT orders_client_order_id_unique UNIQUE (client_order_id)
);

CREATE INDEX idx_orders_transfer_id ON orders (transfer_id);
CREATE INDEX idx_orders_status      ON orders (status) WHERE status IN ('placed', 'partially_filled');

-- Outbox уведомления (Outbox Pattern)
-- Создаются атомарно с ордером, гарантируют доставку даже при падении сервиса
CREATE TABLE outbox_notifications (
    id           UUID                 PRIMARY KEY DEFAULT uuid_generate_v4(),
    transfer_id  UUID                 NOT NULL REFERENCES transfers(id),
    order_id     UUID                 NOT NULL REFERENCES orders(id),
    client_id    TEXT                 NOT NULL,
    channel      notification_channel NOT NULL DEFAULT 'email',
    payload      JSONB                NOT NULL,          -- текст уведомления + детали
    status       notification_status  NOT NULL DEFAULT 'pending',
    retry_count  INT                  NOT NULL DEFAULT 0,
    sent_at      TIMESTAMPTZ,
    created_at   TIMESTAMPTZ          NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ          NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_outbox_status     ON outbox_notifications (status) WHERE status = 'pending';
CREATE INDEX idx_outbox_created_at ON outbox_notifications (created_at ASC);
