package portfolio

import (
	"context"
	"slices"
	"time"

	"tinvest/internal/tinvest"
)

// Источники портфеля. Каждый брокер (Т-Банк, Финам, завтра Сбер) — это Source:
// он отдаёт свои позиции в едином виде (Position), а класс актива определяет не
// брокер, а сама бумага (classify). Поэтому акции и фонды недвижимости могут
// лежать у любого брокера, а новый брокер — это только новый Source.

// Kind — тип инструмента, приведённый к общему виду. Каждый источник переводит в
// него свои типы (instrumentType Т-Банка, type справки Финама).
type Kind string

const (
	KindShare    Kind = "share"    // акция
	KindFund     Kind = "fund"     // пай фонда: ЗПИФ, БПИФ
	KindBond     Kind = "bond"     // облигация
	KindCurrency Kind = "currency" // валюта и драгметаллы на валютной секции
	KindOther    Kind = "other"    // всё прочее и неизвестное
)

// Position — позиция у брокера в едином виде. Все суммы в рублях.
type Position struct {
	// Ticker — биржевой код Мосбиржи (SECID): у Т-Банка это ticker, у Финама —
	// часть символа до «@». По нему позиции разных брокеров складываются в одну
	// строку и сопоставляются со справочниками (realtyFunds, таблицы волта).
	Ticker string
	Kind   Kind
	// UID — идентификатор инструмента в Т-Банке, если источник его знает:
	// справка по акции тогда берётся без поиска по тикеру.
	UID string
	// Name — название от брокера, если он отдаёт его дёшево.
	Name      string
	Value     tinvest.Dec // стоимость позиции
	Price     tinvest.Dec // текущая цена за штуку
	Yield     tinvest.Dec // курсовой доход за всё время
	DayChange tinvest.Dec // изменение за сегодня
}

// SourcePortfolio — вклад одного источника в срез.
type SourcePortfolio struct {
	// Value — полная стоимость счетов брокера: позиции всех типов и деньги.
	Value tinvest.Dec
	// DayChange — изменение за сегодня по счетам брокера.
	DayChange tinvest.Dec
	// Cash — свободные деньги, которые брокер отдаёт не позициями (у Финама —
	// поле cash счёта; у Т-Банка рубли приходят позицией-валютой).
	Cash      tinvest.Dec
	Positions []Position
}

// Source — брокер, из которого собирается портфель.
type Source interface {
	// Name — короткое имя для логов и ошибок (tbank, finam).
	Name() string
	// Portfolio — текущие позиции и итоги по всем счетам брокера.
	Portfolio(ctx context.Context) (*SourcePortfolio, error)
	// Payouts — полученные за всё время выплаты за вычетом налога: дивиденды,
	// купоны, выплаты по паям. В доходность позиций они не входят.
	Payouts(ctx context.Context, now time.Time) (tinvest.Dec, error)
}

// assetClass — класс актива в срезе.
type assetClass int

const (
	classOther assetClass = iota
	classShares
	classGold
	classRealty
	classCash
)

// classify относит позицию к классу по самой бумаге, независимо от брокера.
// Порядок важен: золото и фонды ликвидности API отдают как валюту и фонды, а
// ЗПИФ недвижимости узнаются по справочнику realtyFunds.
func classify(p *Position) assetClass {
	switch {
	case goldTickers[p.Ticker]:
		return classGold
	case cashTickers[p.Ticker]:
		return classCash
	case isRealtyFund(p.Ticker):
		return classRealty
	case p.Kind == KindShare:
		return classShares
	case p.Kind == KindCurrency:
		return classCash
	default:
		return classOther
	}
}

// knownByTicker — класс бумаги определяется по одному тикеру, без типа от
// брокера: золото, фонды ликвидности и ЗПИФ из справочника.
func knownByTicker(ticker string) bool {
	return goldTickers[ticker] || cashTickers[ticker] || isRealtyFund(ticker)
}

// buildSnapshot складывает вклады источников в один срез.
func buildSnapshot(now time.Time, parts []*SourcePortfolio) *Snapshot {
	s := &Snapshot{Date: now}
	for _, part := range parts {
		s.PortfolioValue = s.PortfolioValue.Add(part.Value)
		s.DayChange = s.DayChange.Add(part.DayChange)
		s.Cash = s.Cash.Add(part.Cash)
		for i := range part.Positions {
			s.addPosition(&part.Positions[i])
		}
	}
	s.finish()
	return s
}

// addPosition раскладывает позицию по классу среза.
func (s *Snapshot) addPosition(p *Position) {
	switch classify(p) {
	case classGold:
		s.Gold = s.Gold.Add(p.Value)
		s.GoldYield = s.GoldYield.Add(p.Yield)
		s.GoldDayChange = s.GoldDayChange.Add(p.DayChange)
	case classCash:
		s.Cash = s.Cash.Add(p.Value)
	case classShares:
		s.Shares = s.Shares.Add(p.Value)
		s.StockYield = s.StockYield.Add(p.Yield)
		s.Holdings = mergeHolding(s.Holdings, p)
	case classRealty:
		s.Realty = s.Realty.Add(p.Value)
		s.RealtyYield = s.RealtyYield.Add(p.Yield)
		s.RealtyHoldings = mergeHolding(s.RealtyHoldings, p)
	default:
		// Облигации, прочие фонды и незнакомое: входят в полную стоимость
		// (Value источника), но ни в один класс. Список — для предупреждения в логе.
		if !slices.Contains(s.Unclassified, p.Ticker) {
			s.Unclassified = append(s.Unclassified, p.Ticker)
		}
	}
}

// mergeHolding добавляет позицию в состав класса, складывая её с уже учтённой по
// тому же тикеру: одна бумага у двух брокеров или на двух счетах — одна строка.
// Цена остаётся от первого брокера: у одной бумаги она одна и та же.
func mergeHolding(hs []Holding, p *Position) []Holding {
	for i := range hs {
		h := &hs[i]
		if h.Ticker != p.Ticker {
			continue
		}
		h.Value = h.Value.Add(p.Value)
		h.Yield = h.Yield.Add(p.Yield)
		h.DayChange = h.DayChange.Add(p.DayChange)
		if h.UID == "" {
			h.UID = p.UID
		}
		if h.Name == "" {
			h.Name = p.Name
		}
		return hs
	}
	return append(hs, Holding{
		Ticker:    p.Ticker,
		Name:      p.Name,
		UID:       p.UID,
		Value:     p.Value,
		Price:     p.Price,
		Yield:     p.Yield,
		DayChange: p.DayChange,
	})
}
