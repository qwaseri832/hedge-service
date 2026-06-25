
# 🛡️ Hedge Service

<div align="center">

![Go](https://img.shields.io/badge/Go-1.26-00ADD8?style=for-the-badge&logo=go)
![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-4169E1?style=for-the-badge&logo=postgresql)
![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?style=for-the-badge&logo=docker)
![Prometheus](https://img.shields.io/badge/Prometheus-Monitor-E6522C?style=for-the-badge&logo=prometheus)
![Grafana](https://img.shields.io/badge/Grafana-Dashboards-F46800?style=for-the-badge&logo=grafana)

### ⚡ Автоматическое хеджирование криптовалюты | Go 1.26 + PostgreSQL + Docker

</div>

---

## 📖 О проекте

**Hedge Service** — микросервис на Go, который **автоматически покупает криптовалюту (BTC)** при поступлении фиатного перевода от клиента. Это защищает клиента от потерь на курсовой разнице во время ожидания зачисления.

Сервис использует паттерны распределённых систем: **Outbox Pattern**, **SKIP LOCKED** как очередь, **идемпотентность** и **финансовую точность** через `decimal`.

---

## 🧩 Архитектура

```mermaid
graph TD
    A[Платёжная система] -->|Webhook| B[HTTP Handler]
    B -->|HMAC проверка| C[HedgeUseCase]
    C -->|Сохранение| D[(PostgreSQL)]
    C -->|Размещение ордера| E[Биржа]
    D -->|SKIP LOCKED| F[TransferWorker]
    F -->|Выставление ордера| E
    E -->|Результат| G[(Orders + Outbox)]
    G -->|Атомарная транзакция| D
    D -->|SKIP LOCKED| H[NotificationWorker]
    H -->|Отправка| I[Email/Webhook]
    J[Prometheus] -->|Сбор метрик| K[Grafana]
```

---

## 🔑 Ключевые паттерны

| Паттерн | Описание |
|---------|----------|
| **Outbox Pattern** | Ордер и уведомление создаются атомарно в одной транзакции. Если сервис упадёт после создания ордера, но до отправки уведомления, при рестарте воркер дочитает outbox и отправит его. |
| **SKIP LOCKED (очередь)** | Вместо Redis/Kafka используется `SELECT ... FOR UPDATE SKIP LOCKED`. Несколько воркеров могут параллельно забирать задачи без конфликтов. |
| **Идемпотентность** | `external_id` — уникальный ключ в БД, `client_order_id` — уникальный ключ для биржи. Повторные запросы не создают дубли. |
| **Финансовая точность** | Используется `shopspring/decimal` вместо `float64`, чтобы избежать ошибок округления. Все суммы хранятся как `NUMERIC(20,8)` в PostgreSQL. |
| **Retry с экспоненциальным backoff** | При ошибке биржи воркер повторяет попытку до `MaxRetries`, затем помечает как `failed`. |

---

## 🚀 Быстрый старт

### Запуск через Docker Compose

```bash
# Клонировать репозиторий
git clone https://github.com/qwaseri832/hedge-service.git
cd hedge-service

# Запустить все сервисы (PostgreSQL, миграции, сервис, Prometheus, Grafana)
docker-compose up -d

# Проверить работоспособность
curl http://localhost:8080/health

# Отправить тестовый перевод (USD → BTC)
curl -X POST http://localhost:8080/webhook/transfer \
  -H "Content-Type: application/json" \
  -d '{
    "external_id": "pay-test-001",
    "client_id": "client-123",
    "amount": "500.00",
    "currency": "USD",
    "wallet_addr": "bc1qxy2kgdygjrsqtzq2n0yrf249..."
  }'

# Посмотреть логи
docker-compose logs -f hedge-service
```

### Запуск локально (без Docker)

```bash
# Установить зависимости
go mod download

# Запустить PostgreSQL
docker run -d --name postgres \
  -e POSTGRES_DB=hedge \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=postgres \
  -p 5432:5432 \
  postgres:16-alpine

# Накатить миграции
make migrate-up   # или migrate -path ./migrations -database "postgres://postgres:postgres@localhost:5432/hedge?sslmode=disable" up

# Запустить сервис
go run cmd/main.go
```

### Остановка всех сервисов

```bash
docker-compose down -v   # остановить и удалить данные (опционально)
```

---

## 📡 API

### `POST /webhook/transfer`

Принимает уведомление о входящем переводе.

**Запрос:**

```json
{
  "external_id": "pay-12345",
  "client_id": "client-abc",
  "amount": "500.00",
  "currency": "USD",
  "wallet_addr": "bc1qxy2kgdygjrsqtzq2n0yrf249..."
}
```

**Ответ (201 Accepted):**

```json
{
  "transfer_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "pending"
}
```

**При дубликате (идемпотентность):**

```json
{
  "status": "already_registered"
}
```

---

### `GET /transfers/{id}`

Получение статуса перевода.

**Пример ответа:**

```json
{
  "ID": "550e8400-e29b-41d4-a716-446655440000",
  "ExternalID": "pay-12345",
  "ClientID": "client-abc",
  "AmountFiat": "500",
  "Currency": "USD",
  "WalletAddr": "bc1qxy2kgdygjrsqtzq2n0yrf249...",
  "Status": "processed",
  "RetryCount": 0,
  "CreatedAt": "2026-06-25T09:52:24.805116Z",
  "UpdatedAt": "2026-06-25T09:52:26.159786Z"
}
```

---

### `GET /orders/{id}`

Детали ордера на бирже.

**Пример ответа:**

```json
{
  "ID": "40a6321c-d207-4fe1-ad10-3690278ed124",
  "TransferID": "550e8400-e29b-41d4-a716-446655440000",
  "Symbol": "BTCUSDT",
  "AmountFiat": "500",
  "AmountCrypto": "0.00740001",
  "Price": "67567.50",
  "Status": "filled"
}
```

---

### `GET /health`

Проверка состояния сервиса.

```json
{"status": "ok"}
```

---

### `GET /metrics`

Метрики в формате Prometheus.

---

## 📊 Мониторинг (Prometheus + Grafana)

После запуска через Docker Compose доступны:

| Сервис | URL | Логин / Пароль |
|--------|-----|----------------|
| **Prometheus** | http://localhost:9090 | – |
| **Grafana** | http://localhost:3000 | admin / admin |

Доступные метрики:

| Метрика | Описание |
|---------|----------|
| `hedge_transfers_total` | Количество переводов по статусу |
| `hedge_orders_total` | Количество ордеров по статусу |
| `hedge_order_execution_seconds` | Время выполнения ордера (гистограмма) |
| `hedge_notifications_total` | Количество отправленных уведомлений |
| `hedge_webhook_requests_total` | Количество webhook-запросов по результату |
| `hedge_http_request_duration_seconds` | Время ответа HTTP-эндпоинтов |
| `hedge_pending_transfers` | Текущее количество ожидающих переводов (gauge) |

---

## 🗂 Структура проекта

```
hedge-service/
├── cmd/
│   └── main.go                   # Точка входа, сборка зависимостей
├── config/
│   └── config.go                 # Чтение переменных окружения
├── internal/
│   ├── domain/
│   │   ├── models.go             # Transfer, Order, OutboxNotification
│   │   └── repository.go         # Интерфейсы репозиториев
│   ├── repository/
│   │   └── postgres.go           # Реализация с SKIP LOCKED и Outbox-транзакцией
│   ├── usecase/
│   │   └── hedge.go              # Бизнес-логика (регистрация, обработка)
│   ├── worker/
│   │   ├── transfer_worker.go    # Воркер для переводов
│   │   └── notification_worker.go # Воркер для уведомлений
│   ├── handler/
│   │   └── http.go               # HTTP-обработчики (webhook, status, health, metrics)
│   ├── platform/
│   │   ├── exchange.go           # Интерфейс биржи + Mock
│   │   └── notification.go       # Интерфейс отправки + Mock
│   └── metrics/
│       └── metrics.go            # Prometheus-метрики
├── migrations/
│   ├── 001_init.up.sql
│   └── 001_init.down.sql
├── docker/
│   └── prometheus.yml
├── docker-compose.yml
├── Dockerfile
├── Makefile
├── go.mod
├── go.sum
└── README.md
```

---

## 🔧 Переменные окружения

| Переменная | По умолчанию | Описание |
|------------|--------------|----------|
| `HTTP_ADDR` | `:8080` | Адрес HTTP-сервера |
| `DATABASE_URL` | `postgres://postgres:postgres@localhost:5432/hedge?sslmode=disable` | Строка подключения к PostgreSQL |
| `WEBHOOK_SECRET` | `""` | HMAC-секрет для вебхуков (если пусто — проверка отключена) |
| `TRANSFER_WORKER_COUNT` | `2` | Количество горутин для обработки переводов |
| `NOTIFICATION_WORKER_COUNT` | `1` | Количество горутин для отправки уведомлений |
| `EXCHANGE_FAIL_RATE` | `0.1` | Вероятность ошибки мок-биржи (0.0…1.0) |

---

## 🧪 Тестирование

```bash
# Запуск всех юнит- и интеграционных тестов
make test

# Покрытие кода
make test-cover

# Линтер (требуется golangci-lint)
make lint
```

Юнит-тесты покрывают доменную логику, интеграционные тесты используют **Testcontainers** для поднятия реального PostgreSQL в Docker.

---

## 📦 Основные зависимости

| Библиотека | Назначение |
|------------|------------|
| [pgx](https://github.com/jackc/pgx) | Драйвер для PostgreSQL |
| [shopspring/decimal](https://github.com/shopspring/decimal) | Финансовая точность |
| [prometheus/client_golang](https://github.com/prometheus/client_golang) | Метрики |
| [testcontainers](https://github.com/testcontainers/testcontainers-go) | Интеграционные тесты с реальной БД |
| [google/uuid](https://github.com/google/uuid) | Генерация UUID |

---

## 📝 Лицензия

MIT © 2026

---

<div align="center">

**⭐ Если проект был полезен, поставьте звезду на GitHub!**

</div>
```
