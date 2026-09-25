package portfolio

import (
	"context"
	"fmt"
	"time"

	"tinvest/internal/tinvest"
)

// TBankSource — счета Т-Банка (брокерский и ИИС) через T-Invest API.
type TBankSource struct {
	client *tinvest.Client
}

func NewTBankSource(client *tinvest.Client) *TBankSource {
	return &TBankSource{client: client}
}

func (t *TBankSource) Name() string { return "tbank" }

// Portfolio — один GetPortfolio на счёт. Стоимость и изменение за день берутся
// готовыми итогами счёта: в них входят и позиции, которые ни в один класс не
// попадают (облигации).
func (t *TBankSource) Portfolio(ctx context.Context) (*SourcePortfolio, error) {
	accounts, err := t.accounts(ctx)
	if err != nil {
		return nil, err
	}
	part := &SourcePortfolio{}
	for _, a := range accounts {
		p, err := t.client.GetPortfolio(ctx, a.ID, "RUB")
		if err != nil {
			return nil, err
		}
		part.Value = part.Value.Add(p.TotalAmountPortfolio.Dec())
		part.DayChange = part.DayChange.Add(p.DailyYield.Dec())
		for i := range p.Positions {
			pos := &p.Positions[i]
			part.Positions = append(part.Positions, Position{
				Ticker:    pos.Ticker,
				Kind:      tbankKind(pos.InstrumentType),
				UID:       pos.InstrumentUID,
				Value:     pos.Quantity.Dec().Mul(pos.CurrentPrice.Dec()),
				Price:     pos.CurrentPrice.Dec(),
				Yield:     pos.ExpectedYield.Dec(),
				DayChange: pos.DailyYield.Dec(),
			})
		}
	}
	return part, nil
}

// Payouts — полученные дивиденды по истории операций с даты открытия счетов.
func (t *TBankSource) Payouts(ctx context.Context, now time.Time) (Payouts, error) {
	accounts, err := t.accounts(ctx)
	if err != nil {
		return Payouts{}, err
	}
	return collectDividends(ctx, t.client, accounts, now)
}

// accounts возвращает инвестиционные счета, по которым работаем.
func (t *TBankSource) accounts(ctx context.Context) ([]tinvest.Account, error) {
	all, err := t.client.GetAccounts(ctx)
	if err != nil {
		return nil, err
	}
	targets := selectAccounts(all)
	if len(targets) == 0 {
		return nil, fmt.Errorf("no matching accounts found (available: %d)", len(all))
	}
	return targets, nil
}

// selectAccounts отбирает все инвестиционные счета (брокерский и ИИС).
func selectAccounts(all []tinvest.Account) []tinvest.Account {
	var res []tinvest.Account
	for _, a := range all {
		switch a.Type {
		case "ACCOUNT_TYPE_TINKOFF", "ACCOUNT_TYPE_TINKOFF_IIS":
			res = append(res, a)
		}
	}
	return res
}

// tbankKind переводит instrumentType Т-Банка в общий тип. ЗПИФ и БПИФ Т-Банк
// отдаёт как etf.
func tbankKind(instrumentType string) Kind {
	switch instrumentType {
	case "share":
		return KindShare
	case "etf":
		return KindFund
	case "bond":
		return KindBond
	case "currency":
		return KindCurrency
	default:
		return KindOther
	}
}
