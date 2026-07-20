package main

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"
)

// goldTickers — инструменты, которые считаем золотом. API отдаёт GLDRUB_TOM как
// instrumentType "currency", то есть в totalAmountCurrencies, отдельного класса
// активов для золота в контракте нет.
var goldTickers = map[string]bool{
	"GLDRUB_TOM": true,
	"GLDRUB_TOD": true,
}

// sectorNames — перевод sector из InstrumentsService в названия строк файла.
var sectorNames = map[string]string{
	"energy":       "Энергетика",
	"materials":    "Сырьевая промышленность",
	"financial":    "Финансовый сектор",
	"consumer":     "Потребительские товары",
	"utilities":    "Электроэнергетика",
	"health_care":  "Здравоохранение",
	"real_estate":  "Недвижимость",
	"it":           "Информационные технологии",
	"telecom":      "Телекоммуникации",
	"industrials":  "Промышленность",
	"ecomaterials": "Сырьевая промышленность",
}

type holding struct {
	ticker string
	name   string
	sector string
	value  Dec // стоимость позиции в рублях
	uid    string
}

// snapshot — срез портфеля на момент времени, из которого строятся столбцы таблиц.
type snapshot struct {
	date time.Time
	// total — база для долей: акции + золото. Рублёвый кэш игнорируется,
	// поэтому доли «Акции» и «Золото» в сумме дают ровно 100%.
	total    Dec
	shares   Dec
	gold     Dec
	holdings []holding
	// Абсолютная доходность за всё время, разбитая по классам активов.
	// Для реестра: income = stockYield + goldYield.
	stockYield Dec
	goldYield  Dec
	// dividends — дивидендная доходность за год: сумма yieldValue выплат,
	// то есть к цене закрытия на дату объявления — как в приложении Т-Банка.
	dividends map[string]Dec
}

// collectSnapshot собирает срез по всем указанным счетам.
func collectSnapshot(ctx context.Context, c *Client, accounts []Account, now time.Time) (*snapshot, error) {
	s := &snapshot{date: now, dividends: map[string]Dec{}}

	for _, a := range accounts {
		p, err := c.GetPortfolio(ctx, a.ID, "RUB")
		if err != nil {
			return nil, err
		}
		for _, pos := range p.Positions {
			value := pos.Quantity.Dec().Mul(pos.CurrentPrice.Dec())
			yield := pos.ExpectedYield.Dec()
			switch {
			case goldTickers[pos.Ticker]:
				s.gold = s.gold.Add(value)
				s.goldYield = s.goldYield.Add(yield)
			case pos.InstrumentType == "share":
				s.shares = s.shares.Add(value)
				s.stockYield = s.stockYield.Add(yield)
				s.holdings = append(s.holdings, holding{
					ticker: pos.Ticker,
					value:  value,
					uid:    pos.InstrumentUID,
				})
			}
			// Рублёвый кэш и прочее в базу долей не входят — так считает файл.
		}
	}
	s.total = s.shares.Add(s.gold)

	// Справка по инструментам: название и сектор.
	for i := range s.holdings {
		inst, err := c.ShareByUID(ctx, s.holdings[i].uid)
		if err != nil {
			return nil, fmt.Errorf("справка по %s: %w", s.holdings[i].ticker, err)
		}
		s.holdings[i].name = inst.Name
		s.holdings[i].sector = inst.Sector
	}

	// Дивиденды за текущий календарный год.
	year := now.Year()
	from := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(year, 12, 31, 23, 59, 59, 0, time.UTC)
	for _, h := range s.holdings {
		divs, err := c.Dividends(ctx, h.uid, from, to)
		if err != nil {
			log.Printf("дивиденды %s: %v — пропускаем", h.ticker, err)
			continue
		}
		y := YearDividendYield(divs, year)
		if y.IsZero() {
			continue
		}
		s.dividends[h.ticker] = y
	}

	sort.Slice(s.holdings, func(i, j int) bool {
		return s.holdings[i].value.nanos > s.holdings[j].value.nanos
	})
	return s, nil
}

// columnDate — заголовок нового столбца, в формате уже используемом в файле.
func (s *snapshot) columnDate() string {
	return s.date.Format("2006.01.02")
}

// --- Формирование значений столбцов ---

// pct форматирует долю так же, как в файле: запятая и знак процента.
func pct(d Dec) string {
	return strings.ReplaceAll(d.String(2), ".", ",") + "%"
}

// rub форматирует итог: целые рубли с разделителями тысяч.
func rub(d Dec) string {
	return group(d.String(0))
}

// assetValues — строки таблицы «Актив».
func (s *snapshot) assetValues() (map[string]string, []string) {
	return map[string]string{
		"Акции":     pct(s.shares.Percent(s.total)),
		"Золото":    pct(s.gold.Percent(s.total)),
		"**Всего**": rub(s.total),
	}, []string{"Акции", "Золото", "**Всего**"}
}

// companyValues — доли компаний от общей базы. Ключ — тикер, подпись «TICKER — Название».
func (s *snapshot) companyValues() (byTicker, labels map[string]string, order []string) {
	byTicker = map[string]string{}
	labels = map[string]string{}
	for _, h := range s.holdings {
		byTicker[h.ticker] = pct(h.value.Percent(s.total))
		labels[h.ticker] = fmt.Sprintf("%s — %s", h.ticker, h.name)
		order = append(order, h.ticker)
	}
	return byTicker, labels, order
}

// sectorValues — доли секторов, нормированные к акциям (в сумме 100%).
func (s *snapshot) sectorValues() (map[string]string, []string) {
	sums := map[string]Dec{}
	for _, h := range s.holdings {
		name, ok := sectorNames[h.sector]
		if !ok {
			name = h.sector
			log.Printf("неизвестный сектор %q у %s — строка добавится как есть", h.sector, h.ticker)
		}
		sums[name] = sums[name].Add(h.value)
	}

	values := map[string]string{}
	var order []string
	for name, sum := range sums {
		values[name] = pct(sum.Percent(s.shares))
		order = append(order, name)
	}
	sort.Slice(order, func(i, j int) bool {
		return sums[order[i]].nanos > sums[order[j]].nanos
	})
	return values, order
}

// dividendValues — дивидендная доходность за год, как её показывает Т-Банк.
func (s *snapshot) dividendValues() (byTicker, labels map[string]string, order []string) {
	byTicker = map[string]string{}
	labels = map[string]string{}
	for _, h := range s.holdings {
		labels[h.ticker] = fmt.Sprintf("%s — %s", h.ticker, h.name)
		d, ok := s.dividends[h.ticker]
		if !ok {
			continue
		}
		byTicker[h.ticker] = pct(d)
		order = append(order, h.ticker)
	}
	return byTicker, labels, order
}
