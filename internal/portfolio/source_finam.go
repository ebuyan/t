package portfolio

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"tinvest/internal/finam"
	"tinvest/internal/tinvest"
)

// payoutsSince — запасное начало истории выплат Финама, если API не отдал дату
// открытия счёта. Счёт открыт в 2026 году, раньше выплат быть не может.
var payoutsSince = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// payoutsWindow и payoutsLimit — окно и лимит одного запроса истории. Пагинации у
// Transactions нет, поэтому историю берём помесячно с запасом по лимиту.
const (
	payoutsWindow = 1 // месяцев
	payoutsLimit  = 1000
)

// FinamSource — счета Финама через Finam Trade API.
type FinamSource struct {
	client *finam.Client

	// assets — справка по инструментам (тип, название) по символу. Она не меняется,
	// поэтому живёт весь процесс: GetAsset уходит один раз на новую бумагу.
	mu     sync.Mutex
	assets map[string]*finam.Asset
}

func NewFinamSource(client *finam.Client) *FinamSource {
	return &FinamSource{client: client, assets: map[string]*finam.Asset{}}
}

func (f *FinamSource) Name() string { return "finam" }

// Portfolio — GetAccount по всем счетам токена.
func (f *FinamSource) Portfolio(ctx context.Context) (*SourcePortfolio, error) {
	ids, err := f.client.AccountIDs(ctx)
	if err != nil {
		return nil, err
	}
	part := &SourcePortfolio{}
	for _, id := range ids {
		acc, err := f.client.GetAccount(ctx, id)
		if err != nil {
			return nil, err
		}
		if err := f.addAccount(ctx, acc, part); err != nil {
			return nil, err
		}
	}
	return part, nil
}

// addAccount переводит счёт Финама в общий вид.
func (f *FinamSource) addAccount(ctx context.Context, acc *finam.Account, part *SourcePortfolio) error {
	part.Value = part.Value.Add(acc.Equity.Dec)

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
		part.Cash = part.Cash.Add(v)
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
		p := Position{
			Ticker:    pos.Ticker(),
			Kind:      KindOther,
			Value:     pos.Quantity.Mul(pos.CurrentPrice.Dec),
			Price:     pos.CurrentPrice.Dec,
			Yield:     pos.UnrealizedPnL.Dec,
			DayChange: pos.DailyPnL.Dec,
		}
		a, err := f.asset(ctx, pos.Symbol, acc.AccountID)
		switch {
		case err == nil:
			p.Kind = finamKind(a.Type)
			p.Name = a.Name
		case !knownByTicker(p.Ticker):
			// Без справки класс не определить: акция молча ушла бы «вне классов»
			// и занизила доли и реестр. Лучше ошибка среза — кеш отдаст прошлый.
			return fmt.Errorf("finam asset %s: %w", pos.Symbol, err)
		}
		// Иначе класс известен по тикеру (золото, LQDT, ЗПИФ), справка не нужна.
		part.DayChange = part.DayChange.Add(p.DayChange)
		part.Positions = append(part.Positions, p)
	}
	return nil
}

// asset возвращает справку по инструменту из кеша или API. Удачный ответ живёт
// весь процесс, неудача не кешируется — следующий срез спросит снова.
func (f *FinamSource) asset(ctx context.Context, symbol, accountID string) (*finam.Asset, error) {
	f.mu.Lock()
	a, ok := f.assets[symbol]
	f.mu.Unlock()
	if ok {
		return a, nil
	}
	a, err := f.client.GetAsset(ctx, symbol, accountID)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.assets[symbol] = a
	f.mu.Unlock()
	return a, nil
}

// finamKind переводит тип инструмента из справки Финама в общий тип.
func finamKind(assetType string) Kind {
	switch assetType {
	case "EQUITIES":
		return KindShare
	case "FUNDS", "ETF":
		return KindFund
	case "BONDS":
		return KindBond
	case "CURRENCIES":
		return KindCurrency
	default:
		return KindOther
	}
}

// Payouts суммирует выплаты по всем счетам Финама с даты открытия: дивиденды,
// купоны и выплаты по паям, за вычетом налога.
func (f *FinamSource) Payouts(ctx context.Context, now time.Time) (tinvest.Dec, error) {
	ids, err := f.client.AccountIDs(ctx)
	if err != nil {
		return tinvest.Dec{}, err
	}

	var total tinvest.Dec
	for _, id := range ids {
		txs, err := f.accountTransactions(ctx, id, now)
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
func (f *FinamSource) accountTransactions(ctx context.Context, id string, now time.Time) ([]finam.Transaction, error) {
	acc, err := f.client.GetAccount(ctx, id)
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
		part, err := f.client.Transactions(ctx, id, start, end, payoutsLimit)
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
