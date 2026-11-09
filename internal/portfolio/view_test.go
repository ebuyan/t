package portfolio

import (
	"testing"
	"time"

	"tinvest/internal/tinvest"
)

func TestBuildYieldView(t *testing.T) {
	s := &Snapshot{
		Date:           time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC),
		Shares:         tinvest.DecUnits(1_000_000), // вложено 900 000, доход +100 000 → +11,11%
		Gold:           tinvest.DecUnits(500_000),   // вложено 550 000, доход −50 000 → −9,09%
		StockYield:     tinvest.DecUnits(100_000),
		GoldYield:      tinvest.DecUnits(-50_000),
		Total:          tinvest.DecUnits(1_500_000),
		DayChange:      tinvest.DecUnits(12_000),
		DayChangePct:   tinvest.DecUnits(1),
		GoldDayChange:  tinvest.DecUnits(3_000),
		Cash:           tinvest.DecUnits(7_500),
		PortfolioValue: tinvest.DecUnits(1_507_500), // акции + золото + кеш
		Holdings: []Holding{{
			Ticker:    "SBER",
			Value:     tinvest.DecUnits(600_000),
			UID:       "uid-sber",
			Price:     tinvest.DecUnits(258),
			Yield:     tinvest.DecUnits(60_000),
			DayChange: tinvest.DecUnits(-2_000),
		}},
	}
	m := &Meta{Names: map[string]string{"SBER": "Сбер Банк"}}

	v := BuildYieldView(s, m, Payouts{Total: tinvest.DecUnits(20_000)}, s.Date)

	// «Всего» = полная стоимость портфеля (акции + золото + кеш), не база долей.
	if v.Total != "1 507 500 ₽" {
		t.Errorf("Total = %q, ожидалось %q", v.Total, "1 507 500 ₽")
	}

	// Переоценка = 100 000 − 50 000 = +50 000, плюс 20 000 дивидендов = +70 000;
	// вложено 1 450 000 (переоценка вычтена, дивиденды — нет) → +4,83%.
	if v.Income != "+70 000 ₽" {
		t.Errorf("Income = %q, ожидалось %q", v.Income, "+70 000 ₽")
	}
	if v.IncomePct != "+4,83%" {
		t.Errorf("IncomePct = %q, ожидалось %q", v.IncomePct, "+4,83%")
	}
	if v.Shares.PayoutLabel != "Дивиденды" || v.Shares.Payout != "+20 000 ₽" {
		t.Errorf("дивиденды в карточке акций = %q %q", v.Shares.PayoutLabel, v.Shares.Payout)
	}
	if !v.IncomePos {
		t.Error("IncomePos = false, ожидалось true")
	}
	if v.DayChange != "+12 000 ₽" {
		t.Errorf("DayChange = %q, ожидалось %q", v.DayChange, "+12 000 ₽")
	}
	if v.DayChangePct != "+1,00%" {
		t.Errorf("DayChangePct = %q, ожидалось %q", v.DayChangePct, "+1,00%")
	}
	if !v.DayChangePos {
		t.Error("DayChangePos = false, ожидалось true")
	}
	if v.Shares.YieldPct != "+11,11%" {
		t.Errorf("Shares.YieldPct = %q, ожидалось %q", v.Shares.YieldPct, "+11,11%")
	}
	if v.Gold.Positive {
		t.Error("Gold.Positive = true, ожидалось false (убыток по золоту)")
	}
	if v.Gold.YieldPct != "-9,09%" {
		t.Errorf("Gold.YieldPct = %q, ожидалось %q", v.Gold.YieldPct, "-9,09%")
	}
	// База долей = акции + золото + кеш = 1 507 500; 1 000 000/1 507 500 = 66,33%.
	if v.Shares.Share != "66,33%" {
		t.Errorf("Shares.Share = %q, ожидалось %q", v.Shares.Share, "66,33%")
	}
	// Состав по убыванию изменения за сегодня: золото (+3 000), кеш (0),
	// SBER (−2 000).
	if len(v.Holdings) != 3 {
		t.Fatalf("Holdings: %d строк, ожидалось 3 (акция + золото + кеш): %+v", len(v.Holdings), v.Holdings)
	}
	gold := v.Holdings[0]
	if gold.Name != "Золото" || gold.Value != "500 000 ₽" || gold.Yield != "-50 000 ₽" {
		t.Errorf("строка золота = %+v", gold)
	}
	cash := v.Holdings[1]
	// Кеш теперь с долей от той же базы: 7 500/1 507 500 = 0,50%.
	if cash.Name != "Кеш" || cash.Value != "7 500 ₽" || cash.Share != "0,50%" || cash.Yield != "—" {
		t.Errorf("строка кеша = %+v", cash)
	}
	// Изменение кеша за сегодня — ноль, зелёным; ключа доходности нет.
	if cash.DayChange != "0 ₽" || cash.DayChangeClass != "pos" || cash.DayChangeKey != "0.00" || cash.YieldKey != "" {
		t.Errorf("кеш за сегодня = %+v", cash)
	}
	sber := v.Holdings[2]
	if gold.Class != "gold" || cash.Class != "cash" {
		t.Errorf("классы строк: золото %q, кеш %q", gold.Class, cash.Class)
	}
	// Распределение — в порядке акции, недвижимость (нет), золото, кеш.
	if len(v.Allocation) != 3 || v.Allocation[0].Name != "Акции" || v.Allocation[1].Name != "Золото" ||
		v.Allocation[2].Name != "Кеш" || v.Allocation[2].Share != "0,50%" {
		t.Errorf("распределение = %+v", v.Allocation)
	}
	if v.Today.Amount != v.DayChange || v.AllTime.Pct != v.IncomePct {
		t.Errorf("Today/AllTime = %+v / %+v", v.Today, v.AllTime)
	}
	if sber.Ticker != "SBER" || sber.Value != "600 000 ₽" || sber.Name != "Сбер Банк" || sber.Class != "shares" {
		t.Errorf("строка SBER = %+v", sber)
	}
	if sber.Price != "258,00 ₽" {
		t.Errorf("SBER цена = %q, ожидалось %q", sber.Price, "258,00 ₽")
	}
	if sber.Yield != "+60 000 ₽" || sber.YieldClass != "pos" {
		t.Errorf("SBER доход за всё время = %q (class=%q)", sber.Yield, sber.YieldClass)
	}
	if sber.DayChange != "-2 000 ₽" || sber.DayChangeClass != "neg" {
		t.Errorf("SBER за сегодня = %q (class=%q)", sber.DayChange, sber.DayChangeClass)
	}
	// Ключи сортировки — числа с точкой, без разделителей.
	if sber.ShareKey != "39.80" || sber.YieldKey != "60000.00" || sber.DayChangeKey != "-2000.00" {
		t.Errorf("SBER ключи сортировки = %q %q %q", sber.ShareKey, sber.YieldKey, sber.DayChangeKey)
	}
}

// Без метаданных страница всё равно строится — просто без названий бумаг.
func TestBuildYieldViewNilMeta(t *testing.T) {
	s := &Snapshot{
		Date:     time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC),
		Total:    tinvest.DecUnits(600_000),
		Shares:   tinvest.DecUnits(600_000),
		Holdings: []Holding{{Ticker: "SBER", Value: tinvest.DecUnits(600_000), UID: "uid-sber"}},
	}
	v := BuildYieldView(s, nil, Payouts{}, s.Date)
	if v.Holdings[0].Name != "" {
		t.Errorf("без meta имя должно быть пустым, получили %q", v.Holdings[0].Name)
	}
}

// Недвижимость — своя карточка, фонды — строки состава; доходность считается к
// вложенному в акции + золото + недвижимость.
func TestBuildYieldViewRealty(t *testing.T) {
	s := &Snapshot{
		Shares:      tinvest.DecUnits(600_000),
		StockYield:  tinvest.DecUnits(100_000),
		Realty:      tinvest.DecUnits(400_000),
		RealtyYield: tinvest.DecUnits(-20_000),
		RealtyHoldings: []Holding{{
			Ticker: "RU000A1034U7", Name: "ЗПИФ Современный 7",
			Value: tinvest.DecUnits(400_000), Yield: tinvest.DecUnits(-20_000),
		}},
	}
	s.finish()
	m := &Meta{}

	v := BuildYieldView(s, m, Payouts{Total: tinvest.DecUnits(50_000), Rent: tinvest.DecUnits(30_000)}, time.Now())

	// База долей 1М: недвижимость 40%; доходность −20 000 к вложенным 420 000.
	if v.Realty.Name != "Недвижимость" || v.Realty.Share != "40,00%" || v.Realty.YieldPct != "-4,76%" {
		t.Errorf("карточка недвижимости = %+v", v.Realty)
	}
	// Рента — в карточке недвижимости, дивиденды у акций — за вычетом ренты.
	if v.Realty.PayoutLabel != "Рента" || v.Realty.Payout != "+30 000 ₽" {
		t.Errorf("рента в карточке недвижимости = %q %q", v.Realty.PayoutLabel, v.Realty.Payout)
	}
	if v.Shares.Payout != "+20 000 ₽" {
		t.Errorf("дивиденды в карточке акций = %q, ожидалось +20 000 ₽", v.Shares.Payout)
	}
	// Доход +80 000 переоценки + 50 000 выплат к вложенным 920 000.
	if v.IncomePct != "+14,13%" {
		t.Errorf("IncomePct = %q, ожидалось +14,13%%", v.IncomePct)
	}
	// Полоса распределения: акции, недвижимость; золота и кеша нет — частей нет.
	if len(v.Allocation) != 2 || v.Allocation[0].Class != "shares" || v.Allocation[1].Class != "realty" ||
		v.Allocation[1].Width != "40.00" {
		t.Errorf("распределение = %+v", v.Allocation)
	}
	found := false
	for _, h := range v.Holdings {
		// Тикер — код, название — короткое имя из справочника.
		if h.Ticker == "RU000A1034U7" {
			found = true
			if h.Name != "Современный 7" || h.Share != "40,00%" || h.Class != "realty" {
				t.Errorf("строка фонда = %+v", h)
			}
		}
	}
	if !found {
		t.Error("фонда нет в составе")
	}
}
