package portfolio

import (
	"context"
	"fmt"
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
	// Ticker — биржевой код; по нему позиции разных брокеров складываются в одну
	// строку, а Meta хранит справку.
	Ticker string
	// Name — название от брокера (запасное, если в Meta названия нет).
	Name      string
	Value     tinvest.Dec // стоимость позиции в рублях
	UID       string      // идентификатор Т-Банка, если бумага там есть
	Price     tinvest.Dec // текущая цена за штуку
	Yield     tinvest.Dec // доходность за всё время (expectedYield)
	DayChange tinvest.Dec // изменение за сегодня (dailyYield)
}

// Snapshot — лёгкий срез портфеля по всем источникам (buildSnapshot): суммы по
// классам и состав, без справки по инструментам. Собирается фоном раз в минуту и
// используется веб-страницей и реестром доходности.
type Snapshot struct {
	Date time.Time
	// Total — вложенное в доходные активы: акции + золото + недвижимость. Кеш дохода
	// не даёт и сюда не входит; это знаменатель доходности (Total − Yield()).
	Total  tinvest.Dec
	Shares tinvest.Dec
	Gold   tinvest.Dec
	// Realty — недвижимость: паи ЗПИФ из realtyFunds у любого брокера.
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
	// Holdings — акции у всех брокеров; по ним собираются Meta и таблицы
	// компаний/секторов/дивидендов в Портфель.md.
	Holdings []Holding
	// RealtyHoldings — фонды недвижимости у всех брокеров.
	RealtyHoldings []Holding
	// Unclassified — тикеры позиций вне классов (облигации, прочие фонды): они
	// входят только в PortfolioValue. Список уходит в лог предупреждением.
	Unclassified []string
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
	Names     map[string]string      // тикер → название
	Sectors   map[string]string      // тикер → сектор
	Dividends map[string]tinvest.Dec // тикер → дивидендная доходность за год
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

// collectMeta собирает справку по акциям среза: название, сектор и дивидендную
// доходность за текущий год. Справочник — T-Invest API для любой бумаги
// Мосбиржи: по UID, если акция лежит в Т-Банке, иначе по тикеру. Это дорогая
// часть — ShareBy и GetDividends на каждую бумагу, — поэтому обновляется реже.
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
		inst, err := shareInfo(ctx, c, &h)
		switch {
		case err == nil:
		case h.UID == "":
			// Акция другого брокера, которую справочник T-Invest не нашёл по
			// тикеру (другой режим торгов и т.п.): без названия и сектора, но
			// остальные бумаги справку получат.
			slog.WarnContext(ctx, "share info by ticker failed, skipping",
				slog.String("ticker", h.Ticker), slog.Any("error", err))
			continue
		default:
			return nil, err
		}
		m.Names[h.Ticker] = trimShareSuffix(inst.Name)
		m.Sectors[h.Ticker] = inst.Sector

		divs, err := c.Dividends(ctx, inst.UID, from, to)
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

// shareInfo — справка по акции: по UID Т-Банка, если он есть, иначе по тикеру.
func shareInfo(ctx context.Context, c *tinvest.Client, h *Holding) (*tinvest.Instrument, error) {
	if h.UID != "" {
		inst, err := c.ShareByUID(ctx, h.UID)
		if err != nil {
			return nil, err
		}
		if inst.UID == "" {
			inst.UID = h.UID
		}
		return inst, nil
	}
	inst, err := c.ShareByTicker(ctx, h.Ticker)
	if err != nil {
		return nil, fmt.Errorf("share %s: %w", h.Ticker, err)
	}
	return inst, nil
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
