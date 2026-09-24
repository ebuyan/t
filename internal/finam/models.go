package finam

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"tinvest/internal/tinvest"
)

// Decimal — google.type.Decimal ({"value": "123.45"}), сразу разобранный в Dec:
// деньги нигде не проходят через float.
type Decimal struct {
	tinvest.Dec
}

func (d *Decimal) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		d.Dec = tinvest.Dec{}
		return nil
	}
	var raw struct {
		Value string `json:"value"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return fmt.Errorf("decimal: %w", err)
	}
	v, err := tinvest.ParseDec(raw.Value)
	if err != nil {
		return err
	}
	d.Dec = v
	return nil
}

// Money — google.type.Money: валюта, целая часть (int64 строкой) и нанодоли.
type Money struct {
	CurrencyCode string      `json:"currency_code"`
	Units        json.Number `json:"units"`
	Nanos        int32       `json:"nanos"`
}

// Dec возвращает сумму. Пустая целая часть — ноль.
func (m Money) Dec() (tinvest.Dec, error) {
	var units int64
	if m.Units != "" {
		u, err := strconv.ParseInt(string(m.Units), 10, 64)
		if err != nil {
			return tinvest.Dec{}, fmt.Errorf("money units %q: %w", m.Units, err)
		}
		units = u
	}
	return tinvest.DecParts(units, m.Nanos), nil
}

// IsRUB — рублёвая ли сумма. Пустая валюта считается рублями: так Финам отдаёт
// поля «в рублях» (Transaction.change).
func (m Money) IsRUB() bool {
	return m.CurrencyCode == "" || strings.EqualFold(m.CurrencyCode, "RUB")
}

// Account — ответ GetAccount.
type Account struct {
	AccountID string `json:"account_id"`
	Type      string `json:"type"`
	Status    string `json:"status"`
	// Equity — доступные средства плюс стоимость открытых позиций.
	Equity           Decimal    `json:"equity"`
	UnrealizedProfit Decimal    `json:"unrealized_profit"`
	Positions        []Position `json:"positions"`
	// Cash — собственные деньги на счёте по валютам (без маржинальных).
	Cash            []Money   `json:"cash"`
	OpenAccountDate time.Time `json:"open_account_date"`
}

// Position — позиция счёта. Цена — в валюте инструмента
// (CurrentPriceCurrency), для бумаг Мосбиржи это рубли.
type Position struct {
	// Symbol — инструмент в формате ticker@mic, например SBER@MISX.
	Symbol               string  `json:"symbol"`
	Quantity             Decimal `json:"quantity"`
	AveragePrice         Decimal `json:"average_price"`
	CurrentPrice         Decimal `json:"current_price"`
	DailyPnL             Decimal `json:"daily_pnl"`
	UnrealizedPnL        Decimal `json:"unrealized_pnl"`
	CurrentPriceCurrency string  `json:"current_price_currency"`
}

// Ticker — тикер без биржи: SBER@MISX → SBER.
func (p *Position) Ticker() string { return Ticker(p.Symbol) }

// IsRUB — оценена ли позиция в рублях (пустая валюта — тоже рубли: поле
// заполняется только для счетов Мосбиржи).
func (p *Position) IsRUB() bool {
	return p.CurrentPriceCurrency == "" || strings.EqualFold(p.CurrentPriceCurrency, "RUB")
}

// Ticker отрезает биржу от символа Финама: SBER@MISX → SBER.
func Ticker(symbol string) string {
	t, _, _ := strings.Cut(symbol, "@")
	return t
}

// Категории транзакций (TransactionCategory), нужные для суммы выплат.
const (
	CategoryIncome = "INCOME" // доход: дивиденды, купоны, выплаты по паям
	CategoryTax    = "TAX"    // налог
)

// categoryNames — числовые значения enum TransactionCategory. REST-шлюз обычно
// отдаёт enum строкой, но на число тоже рассчитываем.
var categoryNames = map[int]string{
	0: "OTHERS", 1: "DEPOSIT", 2: "WITHDRAW", 5: CategoryIncome, 7: "COMMISSION",
	8: CategoryTax, 9: "INHERITANCE", 11: "TRANSFER", 12: "CONTRACT_TERMINATION",
	13: "OUTCOMES", 15: "FINE", 19: "LOAN",
}

// Category — категория транзакции, принимает и имя enum, и его номер.
type Category string

func (c *Category) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*c = Category(s)
		return nil
	}
	var n int
	if err := json.Unmarshal(b, &n); err != nil {
		return fmt.Errorf("transaction category: %s", b)
	}
	name, ok := categoryNames[n]
	if !ok {
		name = strconv.Itoa(n)
	}
	*c = Category(name)
	return nil
}

// Transaction — движение по счёту.
type Transaction struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Symbol    string    `json:"symbol"`
	// Change — изменение в деньгах, в рублях.
	Change   Money    `json:"change"`
	Category Category `json:"transaction_category"`
	Name     string   `json:"transaction_name"`
}

// Asset — справка по инструменту.
type Asset struct {
	Symbol string `json:"symbol"`
	Ticker string `json:"ticker"`
	Type   string `json:"type"`
	Name   string `json:"name"`
}
