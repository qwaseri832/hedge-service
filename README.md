<div align="center">

![Go](https://img.shields.io/badge/Go-1.26-00ADD8?style=for-the-badge&logo=go)
![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-4169E1?style=for-the-badge&logo=postgresql)
![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?style=for-the-badge&logo=docker)
![Prometheus](https://img.shields.io/badge/Prometheus-Monitor-E6522C?style=for-the-badge&logo=prometheus)
![Grafana](https://img.shields.io/badge/Grafana-Dashboards-F46800?style=for-the-badge&logo=grafana)

### ⚡ Автоматическое хеджирование криптовалюты | Go 1.26 + PostgreSQL + Docker

</div>

---

## 🎯 Что это такое?

**Hedge Service** — микросервис, который **автоматически покупает криптовалюту (BTC)** при поступлении фиатного перевода от клиента. Это защищает клиента от потерь на курсовой разнице.

---


---


---

## 🔑 Ключевые паттерны

| Паттерн | Описание |
|---------|----------|
| **Outbox Pattern** | Ордер и уведомление создаются атомарно в одной транзакции. Если сервис упадёт — уведомление всё равно будет отправлено при рестарте. |
| **SKIP LOCKED** | PostgreSQL используется как очередь. Несколько воркеров обрабатывают задачи параллельно без конфликтов. |
| **Идемпотентность** | `external_id` и `client_order_id` — уникальные ключи для защиты от дублей. |
| **Финансовая точность** | Используется `decimal` вместо `float64`, чтобы избежать ошибок округления. |

---

## 🚀 Быстрый старт

```bash
# Клонировать
git clone https://github.com/qwaseri832/hedge-service.git
cd hedge-service

# Запустить всё через Docker
docker-compose up -d

# Отправить тестовый перевод
curl -X POST http://localhost:8080/webhook/transfer \
  -H "Content-Type: application/json" \
  -d '{"external_id":"test-1","client_id":"client-1","amount":"500","currency":"USD","wallet_addr":"bc1q..."}'

# Посмотреть логи
docker-compose logs -f hedge-service


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
