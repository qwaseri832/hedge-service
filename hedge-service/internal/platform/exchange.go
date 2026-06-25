package platform

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/shopspring/decimal"
)

// ExchangeClient — интерфейс биржи. В тестах подменяется моком.
type ExchangeClient interface {
	// GetPrice возвращает текущую рыночную цену пары (например BTCUSDT).
	GetPrice(ctx context.Context, symbol string) (decimal.Decimal, error)
	// PlaceMarketOrder размещает рыночный ордер на покупку.
	// clientOrderID — наш идемпотентный ключ.
	// Возвращает exchangeOrderID присвоенный биржей.
	PlaceMarketOrder(ctx context.Context, clientOrderID, symbol string, amountUSD decimal.Decimal) (PlaceOrderResult, error)
	// GetOrderStatus проверяет статус ордера по exchangeOrderID.
	GetOrderStatus(ctx context.Context, symbol, exchangeOrderID string) (OrderStatusResult, error)
}

type PlaceOrderResult struct {
	ExchangeOrderID string
	Symbol          string
	AmountCrypto    decimal.Decimal // сколько крипты куплено
	Price           decimal.Decimal // средняя цена исполнения
	Status          string          // "filled", "partially_filled", "cancelled"
}

type OrderStatusResult struct {
	ExchangeOrderID string
	AmountCrypto    decimal.Decimal
	Price           decimal.Decimal
	Status          string
}

// MockExchangeClient — детерминированный мок для разработки и тестов.
type MockExchangeClient struct {
	FailRate float64 // вероятность ошибки 0.0–1.0
}

func NewMockExchangeClient(failRate float64) ExchangeClient {
	return &MockExchangeClient{FailRate: failRate}
}

func (m *MockExchangeClient) GetPrice(_ context.Context, symbol string) (decimal.Decimal, error) {
	prices := map[string]string{
		"BTCUSDT":  "67500.00",
		"ETHUSDT":  "3500.00",
		"USDTRUB":  "89.50",
	}
	p, ok := prices[symbol]
	if !ok {
		return decimal.Zero, fmt.Errorf("unknown symbol: %s", symbol)
	}
	// Небольшая случайная вариация цены ±0.5%
	base, _ := decimal.NewFromString(p)
	variation := decimal.NewFromFloat(1 + (rand.Float64()-0.5)*0.01)
	return base.Mul(variation).Round(2), nil
}

func (m *MockExchangeClient) PlaceMarketOrder(_ context.Context, clientOrderID, symbol string, amountUSD decimal.Decimal) (PlaceOrderResult, error) {
	if rand.Float64() < m.FailRate {
		return PlaceOrderResult{}, fmt.Errorf("exchange temporarily unavailable")
	}

	// Имитируем цену исполнения
	prices := map[string]decimal.Decimal{
		"BTCUSDT": decimal.NewFromFloat(67500),
		"ETHUSDT": decimal.NewFromFloat(3500),
	}
	price, ok := prices[symbol]
	if !ok {
		return PlaceOrderResult{}, fmt.Errorf("unknown symbol: %s", symbol)
	}

	// Небольшой slippage +0.1%
	slippage := decimal.NewFromFloat(1.001)
	executionPrice := price.Mul(slippage).Round(2)
	amountCrypto := amountUSD.Div(executionPrice).Round(8)

	time.Sleep(50 * time.Millisecond) // имитация сетевого запроса

	return PlaceOrderResult{
		ExchangeOrderID: fmt.Sprintf("EX-%s-%d", clientOrderID[:8], time.Now().UnixNano()),
		Symbol:          symbol,
		AmountCrypto:    amountCrypto,
		Price:           executionPrice,
		Status:          "filled",
	}, nil
}

func (m *MockExchangeClient) GetOrderStatus(_ context.Context, symbol, exchangeOrderID string) (OrderStatusResult, error) {
	if rand.Float64() < m.FailRate {
		return OrderStatusResult{}, fmt.Errorf("exchange temporarily unavailable")
	}
	// В моке всегда возвращаем filled
	return OrderStatusResult{
		ExchangeOrderID: exchangeOrderID,
		Status:          "filled",
	}, nil
}
