-- 001_init.down.sql

DROP TABLE IF EXISTS outbox_notifications;
DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS transfers;

DROP TYPE IF EXISTS notification_channel;
DROP TYPE IF EXISTS notification_status;
DROP TYPE IF EXISTS order_status;
DROP TYPE IF EXISTS transfer_status;
