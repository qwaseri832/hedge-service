Вот чистое README для Hedge Service без лишних вопросов, с картинками и эмодзи:

---

```markdown
# 🛡️ Hedge Service

<div align="center">

**Go 1.22** | **PostgreSQL 16** | **Docker** | **Prometheus** | **Grafana**

![GitHub go.mod Go version](https://img.shields.io/github/go-mod/go-version/qwaseri832/hedge-service)
![License](https://img.shields.io/badge/license-MIT-blue)
![Docker Pulls](https://img.shields.io/badge/docker-ready-brightgreen)

*Микросервис автоматического хеджирования криптовалюты*

</div>

---

## 📖 О проекте

**Hedge Service** — микросервис, который **автоматически покупает криптовалюту (BTC)** при поступлении фиатного перевода от клиента. Это защищает клиента от потерь на курсовой разнице во время ожидания зачисления.

---

## 🎯 Как это работает

```
1. Клиент отправляет $500 USD на счёт компании
            ↓
2. Платёжная система отправляет webhook
            ↓
3. Сервис конвертирует USD в BTC
            ↓
4. Размещается рыночный ордер на бирже
            ↓
5. Клиент получает уведомление
            ↓
6. Криптовалюта поступает на кошелёк ✅
```

---

## 🏗 Архитектура

![Архитектура](docs/architecture.png)

```
┌─────────────────┐
│  Платёжная       │
│  система         │
└────────┬────────┘
         │ Webhook
         ▼
┌─────────────────┐     ┌─────────────────┐
│  HTTP Handler   │────▶│   PostgreSQL    │
└────────┬────────┘     └─────────────────┘
         │
         ▼
┌─────────────────┐     ┌─────────────────┐
│  HedgeUseCase   │────▶│     Биржа       │
└────────┬────────┘     └─────────────────┘
         │
         ▼
┌─────────────────┐     ┌─────────────────┐
│  TransferWorker │────▶│   Outbox        │
└─────────────────┘     └─────────────────┘
         │
         ▼
┌─────────────────┐     ┌─────────────────┐
│ Notification    │────▶│   Email/Webhook │
│ Worker          │     └─────────────────┘
└─────────────────┘
```

---

## 🔑 Ключевые возможности

### Outbox Pattern 📤

Ордер и уведомление создаются атомарно в одной транзакции. Если сервис упадёт — при рестарте уведомление всё равно будет отправлено.

### SKIP LOCKED 🎯

PostgreSQL используется как очередь. Несколько воркеров обрабатывают задачи параллельно без конфликтов.

### Идемпотентность 🔄

Повторные запросы не создают дубли:
- `external_id` — уникальный ключ в БД
- `client_order_id` — уникальный ключ для биржи

### Финансовая точность 💰

Используется `decimal` вместо `float64`:

```go
// ❌ Плохо
amount := 0.1 + 0.2 // 0.30000000000000004

// ✅ Хорошо
amount := decimal.NewFromFloat(0.1).Add(decimal.NewFromFloat(0.2)) // 0.3
```

---

## 🚀 Быстрый старт

### Запуск через Docker

```bash
# Клонировать репозиторий
git clone https://github.com/qwaseri832/hedge-service.git
cd hedge-service

# Запустить все сервисы
docker-compose up -d

# Проверить работу
curl http://localhost:8080/health

# Отправить тестовый перевод
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

### Запуск локально

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
make migrate-up

# Запустить сервис
go run cmd/main.go
```

### Остановка

```bash
docker-compose down
```

---

## 📡 API

### POST /webhook/transfer

Принимает уведомление о переводе.

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

Детали ордера.

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

Проверка состояния.

```json
{"status": "ok"}
```

### GET /metrics

Метрики для Prometheus.

---

## 📊 Метрики

| Метрика | Описание |
|---|---|
| `hedge_transfers_total` | Количество переводов по статусу |
| `hedge_orders_total` | Количество ордеров по статусу |
| `hedge_order_execution_seconds` | Время выполнения ордера |
| `hedge_notifications_total` | Количество уведомлений |
| `hedge_webhook_requests_total` | Количество webhook запросов |
| `hedge_http_request_duration_seconds` | Время ответа HTTP |
| `hedge_pending_transfers` | Текущие ожидающие переводы |

---

## 🗂 Структура проекта

```
hedge-service/
├── cmd/main.go              # Точка входа
├── config/config.go         # Конфигурация
├── internal/
│   ├── domain/              # Сущности и интерфейсы
│   ├── repository/          # Работа с БД
│   ├── usecase/             # Бизнес-логика
│   ├── worker/              # Фоновые воркеры
│   ├── handler/             # HTTP обработчики
│   ├── platform/            # Интеграции (биржа, уведомления)
│   └── metrics/             # Prometheus метрики
├── migrations/              # SQL миграции
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
| `DATABASE_URL` | `postgres://postgres:postgres@localhost:5432/hedge?sslmode=disable` | Подключение к PostgreSQL |
| `WEBHOOK_SECRET` | `""` | HMAC секрет для webhook |
| `TRANSFER_WORKER_COUNT` | `2` | Количество воркеров переводов |
| `NOTIFICATION_WORKER_COUNT` | `1` | Количество воркеров уведомлений |
| `EXCHANGE_FAIL_RATE` | `0.1` | Вероятность ошибки мок-биржи |

---

## 🧪 Тестирование

```bash
# Все тесты
make test

# Покрытие
make test-cover

# Линтер
make lint
```

---

## 📦 Зависимости

| Библиотека | Назначение |
|---|---|
| [pgx](https://github.com/jackc/pgx) | PostgreSQL драйвер |
| [shopspring/decimal](https://github.com/shopspring/decimal) | Финансовая точность |
| [prometheus/client_golang](https://github.com/prometheus/client_golang) | Метрики |
| [testcontainers](https://github.com/testcontainers/testcontainers-go) | Интеграционные тесты |

---

## 🐳 Сервисы Docker

| Сервис | Порт | Назначение |
|---|---|---|
| `postgres` | 5432 | База данных |
| `hedge-service` | 8080 | API сервис |
| `prometheus` | 9090 | Сбор метрик |
| `grafana` | 3000 | Визуализация |

---

## 📝 Лицензия

MIT © 2026

---

<div align="center">

**⭐ Поставьте звезду, если проект полезен!**

</div>
```

---

Это готовое README для **Hedge Service**. Сохраните как `README.md` в корне проекта. Картинку архитектуры (если есть) положите в папку `docs/architecture.png`.
