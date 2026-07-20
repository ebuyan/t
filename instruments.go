package main

import (
	"context"
	"time"
)

// Instrument — справочные данные инструмента из InstrumentsService.
type Instrument struct {
	Ticker string `json:"ticker"`
	Name   string `json:"name"`
	Sector string `json:"sector"`
}

// ShareByUID возвращает справку по акции. Для не-акций (валюта, золото GLDRUB_TOM)
// метод отдаёт ошибку — вызывать его нужно только для instrumentType == "share".
func (c *Client) ShareByUID(ctx context.Context, uid string) (*Instrument, error) {
	var resp struct {
		Instrument Instrument `json:"instrument"`
	}
	req := map[string]string{"idType": "INSTRUMENT_ID_TYPE_UID", "id": uid}
	if err := c.call(ctx, "InstrumentsService", "ShareBy", req, &resp); err != nil {
		return nil, err
	}
	return &resp.Instrument, nil
}

type Dividend struct {
	DividendNet MoneyValue `json:"dividendNet"`
	PaymentDate time.Time  `json:"paymentDate"`
	// YieldValue — доходность выплаты к цене закрытия на дату объявления.
	// Именно её показывает приложение Т-Инвестиций.
	YieldValue Quotation  `json:"yieldValue"`
	ClosePrice MoneyValue `json:"closePrice"`
}

// Dividends возвращает дивидендные выплаты инструмента за период.
func (c *Client) Dividends(ctx context.Context, uid string, from, to time.Time) ([]Dividend, error) {
	var resp struct {
		Dividends []Dividend `json:"dividends"`
	}
	req := map[string]string{
		"instrumentId": uid,
		"from":         from.UTC().Format(time.RFC3339),
		"to":           to.UTC().Format(time.RFC3339),
	}
	if err := c.call(ctx, "InstrumentsService", "GetDividends", req, &resp); err != nil {
		return nil, err
	}
	return resp.Dividends, nil
}

// YearDividendYield суммирует дивидендную доходность выплат за календарный год.
// Берём готовый yieldValue, а не считаем сами: он привязан к цене закрытия на
// дату объявления, и ровно эту цифру показывает приложение Т-Инвестиций.
// Т-Банк может вернуть события за границами запроса, поэтому фильтруем повторно.
func YearDividendYield(divs []Dividend, year int) Dec {
	var sum int64
	for _, d := range divs {
		if d.PaymentDate.Year() == year {
			sum += d.YieldValue.Dec().nanos
		}
	}
	return Dec{nanos: sum}
}
