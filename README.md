Извините! Я перепутал. Вот правильное README для **Hedge Service**:

---

```markdown
# 🛡️ Hedge Service — Automated Crypto Hedging Microservice

<div align="center">

**Go 1.22** | **PostgreSQL 16** | **Docker** | **Prometheus** | **Grafana**

![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/qwaseri832/hedge-service)
![License](https://img.shields.io/badge/license-MIT-blue)
![Docker Pulls](https://img.shields.io/badge/docker-ready-brightgreen)
![Go Report Card](https://goreportcard.com/badge/github.com/qwaseri832/hedge-service)

*Микросервис автоматического хеджирования криптовалюты с паттернами распределённых систем*

</div>

---

## 📖 О проекте

**Hedge Service** — это production-ready микросервис на Go, который **автоматически покупает криптовалюту (BTC)** при поступлении фиатного перевода от клиента.

**Зачем:** чтобы клиент не потерял на курсовой разнице, пока ждёт зачисления. Как только пришёл фиат (USD/RUB) — сервис мгновенно покупает BTC на бирже по текущему курсу и уведомляет клиента.

---

## 🎯 Бизнес-сценарий

```
1. Клиент отправляет $500 USD на счёт компании
            ↓
2. Платёжная система отправляет webhook в Hedge Service
            ↓
3. Сервис конвертирует USD в BTC (если нужно)
            ↓
4. Размещает рыночный ордер на бирже
            ↓
5. Клиент получает уведомление: 
   "Ваша заявка принята. Криптовалюта будет отправлена 
    на ваш кошелёк в течение 10 минут"
            ↓
6. Криптовалюта поступает на кошелёк клиента ✅
```

---

## 🏗 Архитектура

```mermaid
graph TB
    A[Платёжная система] -->|Webhook| B[HTTP Handler]
    B -->|HMAC проверка| C[HedgeUseCase]
    C -->|Сохранение| D[(PostgreSQL)]
    
    E[TransferWorker x2] -->|SKIP LOCKED| D
    E -->|GetPrice| F[Биржа]
    E -->|PlaceOrder| F
    
    F -->|Результат| G[Order + Outbox]
    G -->|Атомарно| D
    
    H[NotificationWorker] -->|SKIP LOCKED| D
    H -->|Отправка| I[Email/Webhook]
    
    J[Prometheus] -->|Сбор метрик| K[Grafana]
```

---

## 🔑 Ключевые паттерны

### 1. Outbox Pattern 📤

Ордер и уведомление создаются в **одной транзакции** атомарно. Если сервис упадёт между созданием ордера и отправкой уведомления — при рестарте воркер дочитает outbox и отправит уведомление.

```sql
BEGIN;
  INSERT INTO orders (...) VALUES (...);
  INSERT INTO outbox_notifications (...) VALUES (...);
COMMIT; -- Атомарно!
```

### 2. SKIP LOCKED как очередь 🎯

Вместо Redis/Kafka используем `SELECT ... FOR UPDATE SKIP LOCKED`. Несколько воркеров могут параллельно забирать задачи без конфликтов.

```sql
SELECT * FROM transfers 
WHERE status = 'pending' 
ORDER BY created_at ASC 
LIMIT 1 
FOR UPDATE SKIP LOCKED;
```

### 3. Идемпотентность 🔄

- `external_id` с `UNIQUE` constraint в БД — повторный webhook возвращает `already_registered`
- `client_order_id = "hedge-{transfer_id}"` — биржа не создаст дубль ордера

### 4. Финансовая точность 💰

Используем `decimal` вместо `float64`. В финансах `0.1 + 0.2` должно быть **точно** `0.3`.

```go
// ❌ Никогда так не делайте в финансах
amount := 0.1 + 0.2 // Может быть 0.30000000000000004

// ✅ Всегда так
amount := decimal.NewFromFloat(0.1).Add(decimal.NewFromFloat(0.2)) // Точно 0.3
```

---

## 🚀 Быстрый старт

### Требования

- Docker & Docker Compose
- Make (опционально)
- Go 1.22+ (для локального запуска)

### Запуск через Docker Compose

```bash
# Клонируйте репозиторий
git clone https://github.com/qwaseri832/hedge-service.git
cd hedge-service

# Запустите все сервисы
docker-compose up -d

# Проверьте здоровье
curl http://localhost:8080/health

# Отправьте тестовый перевод
curl -X POST http://localhost:8080/webhook/transfer \
  -H "Content-Type: application/json" \
  -d '{
    "external_id": "pay-test-001",
    "client_id": "client-123",
    "amount": "500.00",
    "currency": "USD",
    "wallet_addr": "bc1qxy2kgdygjrsqtzq2n0yrf249..."
  }'

# Посмотрите логи
docker-compose logs -f hedge-service
```

### Запуск локально (без Docker)

```bash
# Установите зависимости
go mod download

# Запустите PostgreSQL
docker run -d --name postgres \
  -e POSTGRES_DB=hedge \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=postgres \
  -p 5432:5432 \
  postgres:16-alpine

# Накатите миграции
make migrate-up

# Запустите сервис
go run cmd/main.go
```

---

## 📡 API

### POST /webhook/transfer

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

**Ответ:**

```json
{
  "transfer_id": "550e8400-e29b-41d4-a716-446655440000",
  "status": "pending"
}
```

### GET /transfers/{id}

Статус перевода.

**Ответ:**

```json
{
  "ID": "550e8400-e29b-41d4-a716-446655440000",
  "ExternalID": "pay-12345",
  "ClientID": "client-abc",
  "AmountFiat": "500",
  "Currency": "USD",
  "Status": "processed",
  "RetryCount": 0
}
```

### GET /orders/{id}

Детали ордера на бирже.

**Ответ:**

```json
{
  "ID": "40a6321c-d207-4fe1-ad10-3690278ed124",
  "Symbol": "BTCUSDT",
  "AmountFiat": "500",
  "AmountCrypto": "0.00740001",
  "Price": "67567.50",
  "Status": "filled"
}
```

### GET /health

Health check.

```json
{"status": "ok"}
```

### GET /metrics

Метрики для Prometheus.

---

## 📊 Метрики (Prometheus)

| Метрика | Описание |
|---|---|
| `hedge_transfers_total` | Количество входящих переводов по статусу |
| `hedge_orders_total` | Количество ордеров по статусу |
| `hedge_order_execution_seconds` | Время выполнения ордера |
| `hedge_notifications_total` | Количество отправленных уведомлений |
| `hedge_webhook_requests_total` | Количество webhook запросов |
| `hedge_http_request_duration_seconds` | Время ответа HTTP |
| `hedge_pending_transfers` | Текущее количество ожидающих переводов |

---

## 🗂 Структура проекта

```
hedge-service/
├── cmd/
│   └── main.go              # Точка входа, DI
├── config/
│   └── config.go            # Конфигурация из env
├── internal/
│   ├── domain/
│   │   ├── models.go        # Transfer, Order, OutboxNotification
│   │   └── repository.go    # Интерфейсы репозиториев
│   ├── repository/
│   │   └── postgres.go      # Реализация: SKIP LOCKED, Outbox транзакция
│   ├── usecase/
│   │   └── hedge.go         # Бизнес-логика
│   ├── worker/
│   │   ├── transfer_worker.go      # Обработка переводов
│   │   └── notification_worker.go  # Отправка уведомлений
│   ├── handler/
│   │   └── http.go          # Webhook + status endpoints + HMAC
│   ├── platform/
│   │   ├── exchange.go      # Интерфейс биржи + mock
│   │   └── notification.go  # Интерфейс уведомлений + mock
│   └── metrics/
│       └── metrics.go       # Prometheus метрики
├── migrations/
│   ├── 001_init.up.sql
│   └── 001_init.down.sql
├── docker/
│   └── prometheus.yml
├── docker-compose.yml
├── Dockerfile
├── Makefile
├── go.mod
└── go.sum
```

---

## 🔧 Переменные окружения

| Переменная | По умолчанию | Описание |
|---|---|---|
| `HTTP_ADDR` | `:8080` | Адрес HTTP сервера |
| `DATABASE_URL` | `postgres://postgres:postgres@localhost:5432/hedge?sslmode=disable` | Строка подключения к PostgreSQL |
| `WEBHOOK_SECRET` | `""` | HMAC секрет для валидации webhook |
| `TRANSFER_WORKER_COUNT` | `2` | Количество воркеров для переводов |
| `NOTIFICATION_WORKER_COUNT` | `1` | Количество воркеров для уведомлений |
| `EXCHANGE_FAIL_RATE` | `0.1` | Вероятность ошибки мок-биржи (0.0–1.0) |

---

## 🧪 Тестирование

```bash
# Запуск всех тестов
make test

# Запуск с покрытием
make test-cover

# Линтер
make lint
```

Юнит-тесты покрывают доменную логику без зависимостей от БД или биржи. Включают тест на финансовую точность `decimal` vs `float64`.

---

## 📦 Зависимости

| Библиотека | Назначение |
|---|---|
| [pgx](https://github.com/jackc/pgx) | Драйвер для PostgreSQL |
| [shopspring/decimal](https://github.com/shopspring/decimal) | Финансовая точность |
| [prometheus/client_golang](https://github.com/prometheus/client_golang) | Метрики |
| [testcontainers](https://github.com/testcontainers/testcontainers-go) | Интеграционные тесты |
| [google/uuid](https://github.com/google/uuid) | Генерация UUID |

---

## 🐳 Docker Compose сервисы

| Сервис | Порт | Назначение |
|---|---|---|
| `postgres` | 5432 | Основная БД |
| `hedge-service` | 8080 | API сервис |
| `prometheus` | 9090 | Сбор метрик |
| `grafana` | 3000 | Визуализация (admin/admin) |



---

## 📝 License

MIT © [Ваше имя]

---

## 🙏 Благодарности

- [Shopify](https://github.com/shopspring/decimal) — за `decimal`
- [Prometheus](https://prometheus.io/) — за мониторинг
- [Testcontainers](https://www.testcontainers.org/) — за интеграционные тесты

---

<div align="center">

**⭐ Не забудьте поставить звезду, если проект был полезен!**

</div>
```

---

Это готовое README для **Hedge Service**. Его можно скопировать и сохранить как `README.md` в корне проекта. Картинки вставляются через ссылки на файлы в папке `docs/` или внешние ссылки.
