package portfolio

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"tinvest/internal/finam"
	"tinvest/internal/tinvest"
)

func dec(t *testing.T, s string) finam.Decimal {
	t.Helper()
	d, err := tinvest.ParseDec(s)
	if err != nil {
		t.Fatal(err)
	}
	return finam.Decimal{Dec: d}
}

func pos(t *testing.T, symbol, qty, price, pnl, day string) finam.Position {
	t.Helper()
	return finam.Position{
		Symbol:        symbol,
		Quantity:      dec(t, qty),
		CurrentPrice:  dec(t, price),
		UnrealizedPnL: dec(t, pnl),
		DailyPnL:      dec(t, day),
	}
}

// Счёт Финама: фонды — в недвижимость, золото и LQDT — в свои классы, пустые и
// нерублёвые позиции пропускаются, рублёвый кеш — в кеш.
func TestAddFinamAccount(t *testing.T) {
	acc := &finam.Account{
		AccountID: "A1",
		Equity:    dec(t, "1000000"),
		Cash: []finam.Money{
			{CurrencyCode: "RUB", Units: json.Number("5000")},
			{CurrencyCode: "USD", Units: json.Number("10")},
		},
		Positions: []finam.Position{
			pos(t, "AKCENT@MISX", "344", "870", "-1720", "-344"),
			pos(t, "PARUS@MISX", "260", "1152", "5200", "260"),
			pos(t, "GLDRUB_TOM@MISX", "10", "10000", "1000", "50"),
			pos(t, "LQDT@MISX", "1000", "2", "0", "1"),
			pos(t, "EMPTY@MISX", "0", "100", "0", "0"),
		},
	}
	acc.Positions = append(acc.Positions, finam.Position{
		Symbol: "AAPL@XNGS", Quantity: dec(t, "1"), CurrentPrice: dec(t, "200"), CurrentPriceCurrency: "USD",
	})

	s := &Snapshot{Shares: tinvest.DecUnits(700_000), Gold: tinvest.DecUnits(50_000)}
	if err := addFinamAccount(t.Context(), acc, s); err != nil {
		t.Fatal(err)
	}
	// Тот же фонд на втором счёте складывается в одну строку.
	second := &finam.Account{AccountID: "A2", Positions: []finam.Position{
		pos(t, "AKCENT@MISX", "10", "870", "-50", "-10"),
	}}
	if err := addFinamAccount(t.Context(), second, s); err != nil {
		t.Fatal(err)
	}
	s.finish()

	checks := map[string][2]string{
		"Realty":        {s.Realty.String(0), "607500"}, // 354×870 + 260×1152
		"RealtyYield":   {s.RealtyYield.String(0), "3430"},
		"Gold":          {s.Gold.String(0), "150000"},
		"GoldYield":     {s.GoldYield.String(0), "1000"},
		"Cash":          {s.Cash.String(0), "7000"}, // 5000 ₽ + LQDT 2000
		"DayChange":     {s.DayChange.String(0), "-43"},
		"PortfolioVal":  {s.PortfolioValue.String(0), "1000000"},
		"Total":         {s.Total.String(0), "1457500"}, // акции + золото + недвижимость, без кеша
		"GoldDayChange": {s.GoldDayChange.String(0), "50"},
	}
	for name, c := range checks {
		if c[0] != c[1] {
			t.Errorf("%s = %s, ожидалось %s", name, c[0], c[1])
		}
	}

	if len(s.RealtyHoldings) != 2 {
		t.Fatalf("фондов %d, ожидалось 2: %+v", len(s.RealtyHoldings), s.RealtyHoldings)
	}
	// Сортировка по стоимости: Акцент (307 980) выше Паруса (299 520).
	first := s.RealtyHoldings[0]
	if first.Ticker != "AKCENT" || first.UID != "AKCENT@MISX" || first.Value.String(0) != "307980" {
		t.Errorf("первый фонд = %+v", first)
	}
}

// Новая строка «Недвижимость» встаёт под «Золото», а не в конец под «Всего».
func TestAssetTableInsertsRealty(t *testing.T) {
	doc := strings.Join([]string{
		"| Актив     | 2026.07.24 |",
		"| --------- | ---------- |",
		"| Акции     | 73,58%     |",
		"| Золото    | 19,89%     |",
		"| Кеш       | 6,52%      |",
		"| **Всего** | 5 056 173  |",
		"",
	}, "\n")
	s := testSnap()
	s.Date = time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)

	out, err := applySnapshot(t.Context(), doc, s, &Meta{})
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, l := range strings.Split(out, "\n")[2:] {
		if l == "" {
			continue
		}
		keys = append(keys, strings.TrimSpace(strings.Split(l, "|")[1]))
	}
	want := []string{"Акции", "Золото", "Недвижимость", "Кеш", "**Всего**"}
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Errorf("порядок строк %v, ожидался %v\n%s", keys, want, out)
	}
	// В прошлом столбце новой строки — прочерк; в новом — доля от базы
	// 5М (акции 3.75 + золото 0.75 + недвижимость 0.3 + кеш 0.2).
	if !strings.Contains(out, "| Недвижимость | —          | 6,00%") {
		t.Errorf("строка недвижимости не та:\n%s", out)
	}
	// Повторный прогон не плодит строк.
	again, err := applySnapshot(t.Context(), out, s, &Meta{})
	if err != nil {
		t.Fatal(err)
	}
	if again != out {
		t.Errorf("повторный прогон изменил таблицу:\n%s", again)
	}
}
