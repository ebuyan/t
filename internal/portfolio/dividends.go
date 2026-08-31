package portfolio

import (
	"context"
	"log/slog"
	"time"

	"tinvest/internal/tinvest"
)

// dividendsSince — запасное начало периода, если API не отдал дату открытия
// счёта: раньше этой даты у брокера истории операций всё равно нет.
var dividendsSince = time.Date(2015, 1, 1, 0, 0, 0, 0, time.UTC)

// collectDividends суммирует полученные за всё время дивиденды по всем счетам.
// Это чистая сумма (за вычетом удержанного налога) — ровно то, что пришло
// деньгами и потому не попало в expectedYield позиций.
func collectDividends(
	ctx context.Context, c *tinvest.Client, accounts []tinvest.Account, now time.Time,
) (tinvest.Dec, error) {
	var total tinvest.Dec

	for _, a := range accounts {
		from := a.OpenedDate
		if from.IsZero() || from.Before(dividendsSince) {
			from = dividendsSince
		}

		ops, err := c.OperationsByCursor(ctx, a.ID, from, now, tinvest.DividendOperationTypes())
		if err != nil {
			return tinvest.Dec{}, err
		}

		d := tinvest.SumDividends(ops)
		total = total.Add(d.Net)

		if !d.ToCard.IsZero() {
			slog.WarnContext(ctx, "dividends paid to card are not counted in income",
				slog.String("account_id", a.ID),
				slog.String("amount", d.ToCard.String(2)),
			)
		}
		if len(d.Skipped) > 0 {
			slog.WarnContext(ctx, "non-rub dividend payments skipped",
				slog.String("account_id", a.ID),
				slog.Any("instruments", d.Skipped),
			)
		}
	}
	return total, nil
}
