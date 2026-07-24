package portfolio

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"tinvest/internal/tinvest"
)

// goldTickers — инструменты, которые считаем золотом. API отдаёт GLDRUB_TOM как
// instrumentType "currency", то есть в totalAmountCurrencies, отдельного класса
// активов для золота в контракте нет.
var goldTickers = map[string]bool{
	"GLDRUB_TOM": true,
	"GLDRUB_TOD": true,
}

// Holding — позиция по акции в срезе: тикер, рублёвая стоимость и UID для справки,
// текущая цена за штуку, доходность за всё время и изменение за сегодня.
type Holding struct {
	Ticker    string
	Value     tinvest.Dec // стоимость позиции в рублях
	UID       string
	Price     tinvest.Dec // текущая цена за штуку
	Yield     tinvest.Dec // доходность за всё время (expectedYield)
	DayChange tinvest.Dec // изменение за сегодня (dailyYield)
}

// Snapshot — лёгкий срез портфеля: только суммы и стоимости из GetPortfolio, без
// походов в InstrumentsService. Собирается фоном раз в минуту и используется
// веб-страницей и реестром доходности.
type Snapshot struct {
	Date time.Time
	// Total — база для долей: акции + золото. Рублёвый кэш игнорируется,
	// поэтому доли «Акции» и «Золото» в сумме дают ровно 100%.
	Total  tinvest.Dec
	Shares tinvest.Dec
	Gold   tinvest.Dec
	// Абсолютная доходность за всё время, разбитая по классам активов.
	// Для реестра: income = StockYield + GoldYield.
	StockYield tinvest.Dec
	GoldYield  tinvest.Dec
	// GoldDayChange — изменение по золоту за сегодня (для строки золота в составе).
	GoldDayChange tinvest.Dec
	// Cash — свободные средства (валютные позиции кроме золота, в рублях). Растёт,
	// когда приходят дивиденды, — видно строкой в составе.
	Cash tinvest.Dec
	// Изменение за сегодня по всему портфелю (сумма DailyYield по счетам) —
	// абсолютное в рублях и относительное в процентах, как показывает приложение.
	DayChange    tinvest.Dec
	DayChangePct tinvest.Dec
	// PortfolioValue — полная стоимость портфеля (totalAmountPortfolio: акции,
	// золото, облигации и кэш) на сегодня. В базу долей (Total) не входит кэш, а тут
	// входит всё — это то «всего», что показывает приложение и виджет сводки.
	PortfolioValue tinvest.Dec
	Holdings       []Holding
}

// Meta — тяжёлые справочные данные по инструментам среза: названия и секторы из
// InstrumentsService и дивидендная доходность за год. Меняются редко, поэтому
// обновляются реже среза (см. Cache) и нужны только квартальному срезу долей.
type Meta struct {
	Names     map[string]string      // uid → название
	Sectors   map[string]string      // uid → сектор
	Dividends map[string]tinvest.Dec // тикер → дивидендная доходность за год
}

// collectSnapshot собирает лёгкий срез по всем указанным счетам: один GetPortfolio
// на счёт, без справки по инструментам и дивидендов.
func collectSnapshot(ctx context.Context, c *tinvest.Client, accounts []tinvest.Account, now time.Time) (*Snapshot, error) {
	s := &Snapshot{Date: now}

	// portfolioValue — стоимость всего портфеля (включая кэш и облигации) на сегодня;
	// нужна как знаменатель для относительного изменения за день.
	var portfolioValue tinvest.Dec

	for _, a := range accounts {
		p, err := c.GetPortfolio(ctx, a.ID, "RUB")
		if err != nil {
			return nil, err
		}
		s.DayChange = s.DayChange.Add(p.DailyYield.Dec())
		portfolioValue = portfolioValue.Add(p.TotalAmountPortfolio.Dec())
		for i := range p.Positions {
			pos := &p.Positions[i]
			value := pos.Quantity.Dec().Mul(pos.CurrentPrice.Dec())
			yield := pos.ExpectedYield.Dec()
			day := pos.DailyYield.Dec()
			switch {
			case goldTickers[pos.Ticker]:
				s.Gold = s.Gold.Add(value)
				s.GoldYield = s.GoldYield.Add(yield)
				s.GoldDayChange = s.GoldDayChange.Add(day)
			case pos.InstrumentType == "share":
				s.Shares = s.Shares.Add(value)
				s.StockYield = s.StockYield.Add(yield)
				s.Holdings = append(s.Holdings, Holding{
					Ticker:    pos.Ticker,
					Value:     value,
					UID:       pos.InstrumentUID,
					Price:     pos.CurrentPrice.Dec(),
					Yield:     yield,
					DayChange: day,
				})
			case pos.InstrumentType == "currency":
				// Свободные средства (рубли и прочая валюта в рублёвой оценке);
				// золото сюда не попадает — оно отсечено выше по тикеру.
				s.Cash = s.Cash.Add(value)
			}
			// В базу долей (акции + золото) входят только акции и золото — так
			// считает файл; кеш учитываем отдельной строкой состава.
		}
	}
	s.Total = s.Shares.Add(s.Gold)
	s.PortfolioValue = portfolioValue
	// Относительное изменение — к вчерашней стоимости портфеля (сегодня − изменение).
	s.DayChangePct = s.DayChange.Percent(portfolioValue.Sub(s.DayChange))

	sort.Slice(s.Holdings, func(i, j int) bool {
		return s.Holdings[i].Value.Cmp(s.Holdings[j].Value) > 0
	})
	return s, nil
}

// collectMeta собирает справку по бумагам среза: название, сектор и дивидендную
// доходность за текущий год. Это дорогая часть — ShareBy и GetDividends на каждую
// бумагу, — поэтому она вынесена из collectSnapshot и обновляется реже.
func collectMeta(ctx context.Context, c *tinvest.Client, holdings []Holding) (*Meta, error) {
	m := &Meta{
		Names:     map[string]string{},
		Sectors:   map[string]string{},
		Dividends: map[string]tinvest.Dec{},
	}

	year := time.Now().Year()
	from := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(year, 12, 31, 23, 59, 59, 0, time.UTC)

	for _, h := range holdings {
		inst, err := c.ShareByUID(ctx, h.UID)
		if err != nil {
			return nil, err
		}
		m.Names[h.UID] = inst.Name
		m.Sectors[h.UID] = inst.Sector

		divs, err := c.Dividends(ctx, h.UID, from, to)
		if err != nil {
			slog.WarnContext(ctx, "dividends fetch failed, skipping",
				slog.String("ticker", h.Ticker), slog.Any("error", err))
			continue
		}
		y := tinvest.YearDividendYield(divs, year)
		if y.IsZero() {
			continue
		}
		m.Dividends[h.Ticker] = y
	}
	return m, nil
}

// ColumnDate — заголовок нового столбца, в формате уже используемом в файле.
func (s *Snapshot) ColumnDate() string {
	return s.Date.Format("2006.01.02")
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
