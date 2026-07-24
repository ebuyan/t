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
	m := &Meta{Names: map[string]string{"uid-sber": "Сбер Банк"}}

	v := BuildYieldView(s, m, s.Date)

	// «Всего» = полная стоимость портфеля (акции + золото + кеш), не база долей.
	if v.Total != "1 507 500 ₽" {
		t.Errorf("Total = %q, ожидалось %q", v.Total, "1 507 500 ₽")
	}

	// income = 100 000 − 50 000 = +50 000; вложено 1 450 000 → +3,45%.
	if v.Income != "+50 000 ₽" {
		t.Errorf("Income = %q, ожидалось %q", v.Income, "+50 000 ₽")
	}
	if v.IncomePct != "+3,45%" {
		t.Errorf("IncomePct = %q, ожидалось %q", v.IncomePct, "+3,45%")
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
	sber := v.Holdings[2]
	if sber.Ticker != "SBER" || sber.Value != "600 000 ₽" || sber.Name != "Сбер Банк" {
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
}

// Без метаданных страница всё равно строится — просто без названий бумаг.
func TestBuildYieldViewNilMeta(t *testing.T) {
	s := &Snapshot{
		Date:     time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC),
		Total:    tinvest.DecUnits(600_000),
		Shares:   tinvest.DecUnits(600_000),
		Holdings: []Holding{{Ticker: "SBER", Value: tinvest.DecUnits(600_000), UID: "uid-sber"}},
	}
	v := BuildYieldView(s, nil, s.Date)
	if v.Holdings[0].Name != "" {
		t.Errorf("без meta имя должно быть пустым, получили %q", v.Holdings[0].Name)
	}
}
