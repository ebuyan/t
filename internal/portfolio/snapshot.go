package portfolio

import (
	"context"
	"log/slog"
	"sort"
	"strings"
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

// cashTickers — фонды денежного рынка (БПИФ ликвидности). API отдаёт их как
// instrumentType "etf", отдельного класса активов под фонды нет; по смыслу это
// припаркованные деньги, поэтому считаем их кэшом — идут в строку «Кеш», а в
// знаменатель доходности (Total) не входят.
var cashTickers = map[string]bool{
	"LQDT": true,
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
	// Total — вложенное в доходные активы: акции + золото + недвижимость. Кеш дохода
	// не даёт и сюда не входит; это знаменатель доходности (Total − Yield()).
	Total  tinvest.Dec
	Shares tinvest.Dec
	Gold   tinvest.Dec
	// Realty — недвижимость: паи ЗПИФ на счетах Финама (см. collectFinam).
	Realty tinvest.Dec
	// Абсолютная доходность за всё время, разбитая по классам активов.
	// Для реестра: income = StockYield + GoldYield + RealtyYield + дивиденды.
	StockYield  tinvest.Dec
	GoldYield   tinvest.Dec
	RealtyYield tinvest.Dec
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
	// Holdings — акции со счетов Т-Банка; по ним собираются Meta и таблицы
	// компаний/секторов/дивидендов в Портфель.md.
	Holdings []Holding
	// RealtyHoldings — фонды недвижимости. UID у них — символ Финама
	// (ticker@mic): по нему Meta хранит название фонда.
	RealtyHoldings []Holding
}

// Yield — курсовой доход за всё время по всем классам (без дивидендов и выплат:
// те приходят деньгами и в переоценку позиций не попадают).
func (s *Snapshot) Yield() tinvest.Dec {
	return s.StockYield.Add(s.GoldYield).Add(s.RealtyYield)
}

// Meta — тяжёлые справочные данные по инструментам среза: названия и секторы из
// InstrumentsService и дивидендная доходность за год. Меняются редко, поэтому
// обновляются реже среза (см. Cache) и нужны только квартальному срезу долей.
type Meta struct {
	Names     map[string]string      // uid → название
	Sectors   map[string]string      // uid → сектор
	Dividends map[string]tinvest.Dec // тикер → дивидендная доходность за год
}

// collectSnapshot добавляет в срез счета Т-Банка: один GetPortfolio на счёт, без
// справки по инструментам и дивидендов. Итоги считает finish.
func collectSnapshot(ctx context.Context, c *tinvest.Client, accounts []tinvest.Account, s *Snapshot) error {
	for _, a := range accounts {
		p, err := c.GetPortfolio(ctx, a.ID, "RUB")
		if err != nil {
			return err
		}
		s.DayChange = s.DayChange.Add(p.DailyYield.Dec())
		s.PortfolioValue = s.PortfolioValue.Add(p.TotalAmountPortfolio.Dec())
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
			case cashTickers[pos.Ticker]:
				// Фонды денежного рынка (LQDT) — считаем кэшом.
				s.Cash = s.Cash.Add(value)
			case pos.InstrumentType == "currency":
				// Свободные средства (рубли и прочая валюта в рублёвой оценке);
				// золото сюда не попадает — оно отсечено выше по тикеру.
				s.Cash = s.Cash.Add(value)
			}
		}
	}
	return nil
}

// finish считает итоги среза, когда все брокеры уже сложены.
func (s *Snapshot) finish() {
	s.Total = s.Shares.Add(s.Gold).Add(s.Realty)
	// Относительное изменение — к вчерашней стоимости портфеля (сегодня − изменение).
	s.DayChangePct = s.DayChange.Percent(s.PortfolioValue.Sub(s.DayChange))

	byValue := func(h []Holding) func(i, j int) bool {
		return func(i, j int) bool { return h[i].Value.Cmp(h[j].Value) > 0 }
	}
	sort.Slice(s.Holdings, byValue(s.Holdings))
	sort.Slice(s.RealtyHoldings, byValue(s.RealtyHoldings))
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
		m.Names[h.UID] = trimShareSuffix(inst.Name)
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

// trimShareSuffix убирает хвост «- акции привилегированные» из названия бумаги
// (API отдаёт его в inst.Name) — в подписях он лишний, тикер и так это показывает.
func trimShareSuffix(name string) string {
	trimmed := strings.TrimSpace(name)
	trimmed, _ = strings.CutSuffix(trimmed, " - акции привилегированные")
	trimmed, _ = strings.CutSuffix(trimmed, " - привилегированные акции")
	return trimmed
}

// ShareBase — база для долей на странице, в виджете меню-бара и в таблице «Актив»
// Портфель.md: акции + золото + недвижимость + кеш. В сумме доли классов дают
// ровно 100%.
func (s *Snapshot) ShareBase() tinvest.Dec {
	return s.Shares.Add(s.Gold).Add(s.Realty).Add(s.Cash)
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
