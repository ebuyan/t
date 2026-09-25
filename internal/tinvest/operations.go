package tinvest

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Дивиденды не входят в expectedYield позиции: выплата приходит деньгами и
// увеличивает кэш, а доходность позиции считается только от цены. Поэтому
// фактически полученные выплаты берём из истории операций.
const (
	// opTypeDividend — зачисление дивидендов на счёт.
	opTypeDividend = "OPERATION_TYPE_DIVIDEND"
	// opTypeDividendTax — удержанный с дивидендов налог (payment отрицательный).
	opTypeDividendTax = "OPERATION_TYPE_DIVIDEND_TAX"
	// opTypeDividendTaxProgressive — тот же налог по прогрессивной ставке.
	opTypeDividendTaxProgressive = "OPERATION_TYPE_DIVIDEND_TAX_PROGRESSIVE"
	// opTypeTaxCorrectionDividend — корректировка ранее удержанного налога.
	opTypeTaxCorrectionDividend = "OPERATION_TYPE_TAX_CORRECTION_DIVIDEND"
	// opTypeDivExt — выплата дивидендов на карту, мимо брокерского счёта.
	opTypeDivExt = "OPERATION_TYPE_DIV_EXT"
)

// dividendNetTypes — операции, из которых складывается чистая выплата «на руки»:
// зачисление минус удержанный налог (у налога payment уже отрицательный).
var dividendNetTypes = []string{
	opTypeDividend,
	opTypeDividendTax,
	opTypeDividendTaxProgressive,
	opTypeTaxCorrectionDividend,
}

// DividendOperationTypes — типы операций, которые запрашиваем у API. Выплату на
// карту (opTypeDivExt) тянем тоже, но в сумму не кладём — см. Dividends.ToCard.
func DividendOperationTypes() []string {
	return append(slices.Clone(dividendNetTypes), opTypeDivExt)
}

// maxOperationPages — предохранитель от бесконечной пагинации: при limit=1000
// это 100 тысяч операций, что заведомо больше любой реальной истории счёта.
const maxOperationPages = 100

// operationPageLimit — размер страницы, максимум по контракту API.
const operationPageLimit = 1000

// Operation — операция по счёту (OperationItem из GetOperationsByCursor).
// Поле type — enum OperationType, payment — сумма операции со знаком.
type Operation struct {
	ID      string     `json:"id"`
	Type    string     `json:"type"`
	Date    time.Time  `json:"date"`
	Ticker  string     `json:"ticker"`
	Figi    string     `json:"figi"`
	Payment MoneyValue `json:"payment"`
}

func (o *Operation) label() string {
	switch {
	case o.Ticker != "":
		return o.Ticker
	case o.Figi != "":
		return o.Figi
	default:
		return o.ID
	}
}

// OperationsByCursor возвращает исполненные операции указанных типов за период,
// самостоятельно обходя пагинацию. Сделки внутри операций не запрашиваем — для
// сумм выплат они не нужны и только раздувают ответ.
func (c *Client) OperationsByCursor(
	ctx context.Context, accountID string, from, to time.Time, types []string,
) ([]Operation, error) {
	var all []Operation
	cursor := ""

	for page := 0; page < maxOperationPages; page++ {
		req := map[string]any{
			"accountId":      accountID,
			"from":           from.UTC().Format(time.RFC3339),
			"to":             to.UTC().Format(time.RFC3339),
			"limit":          operationPageLimit,
			"operationTypes": types,
			"state":          "OPERATION_STATE_EXECUTED",
			"withoutTrades":  true,
		}
		if cursor != "" {
			req["cursor"] = cursor
		}

		var resp struct {
			Items      []Operation `json:"items"`
			HasNext    bool        `json:"hasNext"`
			NextCursor string      `json:"nextCursor"`
		}
		if err := c.call(ctx, "OperationsService", "GetOperationsByCursor", req, &resp); err != nil {
			return nil, err
		}
		all = append(all, resp.Items...)

		if !resp.HasNext || resp.NextCursor == "" {
			return all, nil
		}
		cursor = resp.NextCursor
	}
	return nil, fmt.Errorf("operations pagination exceeded %d pages", maxOperationPages)
}

// Dividends — итог по дивидендным операциям счёта.
type Dividends struct {
	// Net — чистая сумма, зачисленная на счёт: выплаты минус удержанный налог.
	Net Dec
	// ByTicker — Net в разрезе тикера бумаги (у операции без тикера — ключ "").
	ByTicker map[string]Dec
	// ToCard — выплаты, ушедшие сразу на карту (opTypeDivExt). В Net не входят:
	// по одной операции не понять, дублирует ли она зачисление на счёт, а тихо
	// завысить доход хуже, чем показать его без этих выплат.
	ToCard Dec
	// Skipped — операции не в рублях: складывать разные валюты нельзя, поэтому
	// возвращаем их отдельно, а не молча занижаем сумму (как в TotalYield).
	Skipped []string
}

// SumDividends раскладывает операции по итогу. Ожидает уже отфильтрованный
// ответ API (типы из DividendOperationTypes); чужие типы игнорирует.
func SumDividends(ops []Operation) Dividends {
	d := Dividends{ByTicker: map[string]Dec{}}
	for i := range ops {
		op := &ops[i]
		if cur := op.Payment.Currency; cur != "" && !strings.EqualFold(cur, "rub") {
			d.Skipped = append(d.Skipped, op.label())
			continue
		}
		switch {
		case slices.Contains(dividendNetTypes, op.Type):
			d.Net = d.Net.Add(op.Payment.Dec())
			d.ByTicker[op.Ticker] = d.ByTicker[op.Ticker].Add(op.Payment.Dec())
		case op.Type == opTypeDivExt:
			d.ToCard = d.ToCard.Add(op.Payment.Dec())
		}
	}
	return d
}
