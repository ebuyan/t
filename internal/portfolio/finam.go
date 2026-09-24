package portfolio

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"tinvest/internal/finam"
	"tinvest/internal/tinvest"
)

// Счета Финама. Там лежат паи ЗПИФ недвижимости, поэтому всё, что на Финаме не
// золото и не кеш, считается классом «Недвижимость». Если на Финам придут акции,
// классификацию придётся уточнить по типу инструмента (GetAsset).

// payoutsSince — запасное начало истории выплат, если API не отдал дату открытия
// счёта. Счёт открыт в 2026 году, раньше выплат быть не может.
var payoutsSince = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// payoutsWindow и payoutsLimit — окно и лимит одного запроса истории. Пагинации у
// Transactions нет, поэтому историю берём помесячно с запасом по лимиту.
const (
	payoutsWindow = 1 // месяцев
	payoutsLimit  = 1000
)

// collectFinam добавляет в срез счета Финама.
func collectFinam(ctx context.Context, c *finam.Client, s *Snapshot) error {
	ids, err := c.AccountIDs(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		acc, err := c.GetAccount(ctx, id)
		if err != nil {
			return err
		}
		if err := addFinamAccount(ctx, acc, s); err != nil {
			return err
		}
	}
	return nil
}

// addFinamAccount раскладывает один счёт Финама по классам среза.
func addFinamAccount(ctx context.Context, acc *finam.Account, s *Snapshot) error {
	s.PortfolioValue = s.PortfolioValue.Add(acc.Equity.Dec)

	for _, m := range acc.Cash {
		if !m.IsRUB() {
			slog.WarnContext(ctx, "non-rub cash on finam account skipped",
				slog.String("account_id", acc.AccountID), slog.String("currency", m.CurrencyCode))
			continue
		}
		v, err := m.Dec()
		if err != nil {
			return fmt.Errorf("finam account %s cash: %w", acc.AccountID, err)
		}
		s.Cash = s.Cash.Add(v)
	}

	for i := range acc.Positions {
		pos := &acc.Positions[i]
		if pos.Quantity.IsZero() {
			continue
		}
		if !pos.IsRUB() {
			slog.WarnContext(ctx, "non-rub position on finam account skipped",
				slog.String("account_id", acc.AccountID), slog.String("symbol", pos.Symbol),
				slog.String("currency", pos.CurrentPriceCurrency))
			continue
		}
		value := pos.Quantity.Mul(pos.CurrentPrice.Dec)
		yield := pos.UnrealizedPnL.Dec
		day := pos.DailyPnL.Dec
		ticker := pos.Ticker()
		s.DayChange = s.DayChange.Add(day)

		switch {
		case goldTickers[ticker]:
			s.Gold = s.Gold.Add(value)
			s.GoldYield = s.GoldYield.Add(yield)
			s.GoldDayChange = s.GoldDayChange.Add(day)
		case cashTickers[ticker]:
			s.Cash = s.Cash.Add(value)
		default:
			s.Realty = s.Realty.Add(value)
			s.RealtyYield = s.RealtyYield.Add(yield)
			s.RealtyHoldings = addHolding(s.RealtyHoldings, Holding{
				Ticker:    ticker,
				Value:     value,
				UID:       pos.Symbol,
				Price:     pos.CurrentPrice.Dec,
				Yield:     yield,
				DayChange: day,
			})
		}
	}
	return nil
}

// addHolding добавляет позицию, складывая её с уже учтённой по тому же символу
// (один фонд на двух счетах — одна строка). Цена у них общая.
func addHolding(hs []Holding, h Holding) []Holding {
	for i := range hs {
		if hs[i].UID == h.UID {
			hs[i].Value = hs[i].Value.Add(h.Value)
			hs[i].Yield = hs[i].Yield.Add(h.Yield)
			hs[i].DayChange = hs[i].DayChange.Add(h.DayChange)
			return hs
		}
	}
	return append(hs, h)
}

// collectFinamNames добавляет в meta названия фондов недвижимости. Название —
// только подпись, поэтому ошибка по отдельной бумаге не валит сбор.
func collectFinamNames(ctx context.Context, c *finam.Client, holdings []Holding, m *Meta) {
	if len(holdings) == 0 {
		return
	}
	ids, err := c.AccountIDs(ctx)
	if err != nil || len(ids) == 0 {
		slog.WarnContext(ctx, "finam accounts unavailable, fund names skipped", slog.Any("error", err))
		return
	}
	for _, h := range holdings {
		a, err := c.GetAsset(ctx, h.UID, ids[0])
		if err != nil {
			slog.WarnContext(ctx, "finam asset fetch failed, skipping",
				slog.String("symbol", h.UID), slog.Any("error", err))
			continue
		}
		m.Names[h.UID] = a.Name
	}
}

// collectFinamPayouts суммирует выплаты по всем счетам Финама с даты открытия:
// дивиденды, купоны и выплаты по паям ЗПИФ, за вычетом налога.
func collectFinamPayouts(ctx context.Context, c *finam.Client, now time.Time) (tinvest.Dec, error) {
	ids, err := c.AccountIDs(ctx)
	if err != nil {
		return tinvest.Dec{}, err
	}

	var total tinvest.Dec
	for _, id := range ids {
		txs, err := accountTransactions(ctx, c, id, now)
		if err != nil {
			return tinvest.Dec{}, err
		}
		p := finam.SumPayouts(txs)
		total = total.Add(p.Net)
		if len(p.Skipped) > 0 {
			slog.WarnContext(ctx, "finam payouts skipped",
				slog.String("account_id", id), slog.Any("symbols", p.Skipped))
		}
	}
	return total, nil
}

// accountTransactions выгружает всю историю транзакций счёта с даты открытия,
// помесячными окнами.
func accountTransactions(ctx context.Context, c *finam.Client, id string, now time.Time) ([]finam.Transaction, error) {
	acc, err := c.GetAccount(ctx, id)
	if err != nil {
		return nil, err
	}
	from := acc.OpenAccountDate
	if from.IsZero() || from.Before(payoutsSince) {
		from = payoutsSince
	}

	var txs []finam.Transaction
	for start := from; start.Before(now); start = start.AddDate(0, payoutsWindow, 0) {
		end := start.AddDate(0, payoutsWindow, 0)
		if end.After(now) {
			end = now
		}
		part, err := c.Transactions(ctx, id, start, end, payoutsLimit)
		if err != nil {
			return nil, err
		}
		if len(part) >= payoutsLimit {
			slog.WarnContext(ctx, "finam transactions hit the limit, payouts may be incomplete",
				slog.String("account_id", id), slog.Time("from", start), slog.Time("to", end))
		}
		txs = append(txs, part...)
	}
	return txs, nil
}
