# Hedge Service

Микросервис автоматического хеджирования. При поступлении фиатного перевода
сразу покупает криптовалюту на бирже по текущему курсу — чтобы не потерять
на курсовой разнице пока клиент ждёт.

После исполнения ордера клиент автоматически получает уведомление:
*«Ваша заявка принята. Криптовалюта будет отправлена на ваш кошелёк в течение 10 минут.»*

## Архитектура

```
POST /webhook/transfer
         │
         ▼
  [HTTP Handler]         ← проверяет HMAC подпись
         │
         ▼
  [HedgeUseCase]         ← сохраняет Transfer в Postgres
         │
    transfers (pending)
         │
         ▼  SKIP LOCKED
  [TransferWorker x2]    ← конкурентно, без Redis
         │
         ├─ GetPrice(BTCUSDT)   ← биржа
         ├─ PlaceMarketOrder()  ← биржа (с clientOrderID для идемпотентности)
         │
         ▼  одна транзакция
  ┌──────────────────┐
  │  INSERT orders   │   ← Outbox Pattern
  │  INSERT outbox   │   ← атомарно вместе с ордером
  └──────────────────┘
         │
    outbox_notifications (pending)
         │
         ▼  SKIP LOCKED
  [NotificationWorker]   ← читает outbox, отправляет email/webhook
```

## Ключевые паттерны

### Idempotency
- Webhook: `external_id` с `UNIQUE` constraint — повторный запрос возвращает `already_registered`
- Биржа: `client_order_id = "hedge-{transfer_id}"` — биржа не создаст дубль ордера

### Outbox Pattern
Ордер и уведомление создаются в **одной транзакции**. Даже если сервис упадёт
между созданием ордера и отправкой уведомления — при рестарте воркер дочитает
outbox и уведомление уйдёт. Dual write исключён.

### SKIP LOCKED как очередь
Вместо Redis или Kafka — Postgres с `SELECT ... FOR UPDATE SKIP LOCKED`.
Несколько горутин-воркеров берут задачи параллельно без конфликтов.
Это production-паттерн, который используют Shopify, GitHub, Basecamp.

### Финансовая точность
`shopspring/decimal` вместо `float64`. `0.1 + 0.2 = 0.3` — всегда точно.
Все суммы хранятся как `NUMERIC(20,8)` в Postgres.

### Retry с экспоненциальным backoff
Биржа может быть недоступна. Воркер повторяет попытку до `MaxRetries` раз,
инкрементируя `retry_count`. При исчерпании — статус `failed`.

## Быстрый старт

```bash
# Запустить всё
make up

# Подождать 10 секунд, затем отправить тестовый перевод
make send-test

# Посмотреть логи сервиса
make logs
```

## API

### POST /webhook/transfer
Принимает уведомление о входящем переводе.

```bash
curl -X POST http://localhost:8080/webhook/transfer \
  -H "Content-Type: application/json" \
  -H "X-Signature: <hmac-sha256>" \
  -d '{
    "external_id": "pay-12345",
    "client_id": "client-abc",
    "amount": "500.00",
    "currency": "USD",
    "wallet_addr": "bc1qxy2kgdygjrsqtzq2n0yrf249..."
  }'
```

Ответ:
```json
{"transfer_id": "uuid", "status": "pending"}
```

### GET /transfers/{id}
Статус перевода.

### GET /orders/{id}
Детали ордера: цена исполнения, количество крипты, статус.

### GET /health
Health check.

## Структура

```
hedge-service/
├── cmd/main.go                      # точка входа, wire всех зависимостей
├── config/config.go                 # конфиг из env
├── internal/
│   ├── domain/
│   │   ├── models.go                # Transfer, Order, OutboxNotification
│   │   └── repository.go           # интерфейсы репозиториев
│   ├── repository/
│   │   └── postgres.go             # реализация: SKIP LOCKED, Outbox транзакция
│   ├── usecase/
│   │   └── hedge.go                # бизнес-логика: регистрация, ProcessTransfer
│   ├── worker/
│   │   ├── transfer_worker.go      # polling pending переводов
│   │   └── notification_worker.go  # polling outbox уведомлений
│   ├── handler/
│   │   └── http.go                 # webhook + status endpoints + HMAC verify
│   └── platform/
│       ├── exchange.go             # интерфейс биржи + mock
│       └── notification.go         # интерфейс отправки + mock
├── migrations/
│   ├── 001_init.up.sql
│   └── 001_init.down.sql
├── docker-compose.yml
├── Dockerfile
└── Makefile
```

## Переменные окружения

| Переменная | По умолчанию | Описание |
|---|---|---|
| `HTTP_ADDR` | `:8080` | Адрес HTTP сервера |
| `DATABASE_URL` | `postgres://...` | Строка подключения к Postgres |
| `WEBHOOK_SECRET` | `""` | HMAC секрет для валидации webhook |
| `TRANSFER_WORKER_COUNT` | `2` | Число горутин-воркеров для переводов |
| `NOTIFICATION_WORKER_COUNT` | `1` | Число воркеров для уведомлений |
| `EXCHANGE_FAIL_RATE` | `0.1` | Вероятность ошибки мок-биржи (0.0–1.0) |

## Тесты

```bash
make test
```

Юнит-тесты покрывают доменную логику без зависимостей от БД или биржи.
Включают тест на финансовую точность decimal vs float64.

## Вопросы на собеседовании

**Q: Почему не Kafka для очереди?**
A: Для одного микросервиса с одним типом событий Kafka — оверинжиниринг.
Postgres с SKIP LOCKED даёт те же гарантии at-least-once, проще операционно,
и не требует отдельной инфраструктуры. При росте нагрузки легко мигрировать.

**Q: Что такое Outbox Pattern и зачем?**
A: Проблема dual write: если сохранить ордер в БД и потом попытаться отправить
уведомление — между этими операциями сервис может упасть. Outbox решает это:
ордер и событие уведомления пишутся атомарно в одной транзакции. Отдельный воркер
читает outbox и отправляет. Гарантирует at-least-once доставку.

**Q: Как обеспечивается идемпотентность ордера на бирже?**
A: У каждого ордера есть `client_order_id = "hedge-{transfer_id}"`. Если сервис
упал после PlaceMarketOrder но до сохранения результата — при retry биржа вернёт
тот же ордер по clientOrderID, дубля не будет. Мы также проверяем через
GetByClientOrderID перед каждым размещением.

**Q: Почему decimal а не float64?**
A: float64 не может точно представить 0.1. `0.1 + 0.2 != 0.3` в float64.
В финансовых операциях это недопустимо — можно потерять деньги на округлениях.
decimal хранит числа точно, как в математике.
