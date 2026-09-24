package portfolio

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"tinvest/internal/finam"
	"tinvest/internal/tinvest"
)

func dec(t *testing.T, s string) tinvest.Dec {
	t.Helper()
	d, err := tinvest.ParseDec(s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func position(t *testing.T, ticker string, kind Kind, value, yield, day string) Position {
	t.Helper()
	return Position{Ticker: ticker, Kind: kind, Value: dec(t, value), Yield: dec(t, yield), DayChange: dec(t, day)}
}

// Класс определяет бумага, а не брокер: акции и фонды недвижимости могут лежать у
// любого источника, одна бумага у двух брокеров — одна строка.
func TestBuildSnapshotMixedSources(t *testing.T) {
	tbank := &SourcePortfolio{
		Value:     dec(t, "1000000"),
		DayChange: dec(t, "1000"),
		Positions: []Position{
			position(t, "SBER", KindShare, "600000", "60000", "-2000"),
			position(t, "GLDRUB_TOM", KindCurrency, "200000", "10000", "500"),
			position(t, "RUB000UTSTOM", KindCurrency, "50000", "0", "0"),
			position(t, "LQDT", KindFund, "30000", "100", "10"),
			// ЗПИФ недвижимости в Т-Банке — тоже недвижимость.
			position(t, "RU000A104172", KindFund, "100000", "-5000", "100"),
			position(t, "SU26238RMFS4", KindBond, "20000", "0", "0"),
		},
	}
	fin := &SourcePortfolio{
		Value:     dec(t, "700000"),
		DayChange: dec(t, "-300"),
		Cash:      dec(t, "7000"),
		Positions: []Position{
			// Та же акция у другого брокера складывается с позицией Т-Банка.
			position(t, "SBER", KindShare, "100000", "5000", "-400"),
			position(t, "XACCSK", KindFund, "300000", "-3000", "-200"),
			// Фонд не из справочника в класс не попадает.
			position(t, "UNKNOWNFUND", KindFund, "293000", "0", "0"),
		},
	}

	s := buildSnapshot(time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC), []*SourcePortfolio{tbank, fin})

	checks := map[string][2]string{
		"Shares":         {s.Shares.String(0), "700000"},
		"StockYield":     {s.StockYield.String(0), "65000"},
		"Gold":           {s.Gold.String(0), "200000"},
		"Realty":         {s.Realty.String(0), "400000"},
		"RealtyYield":    {s.RealtyYield.String(0), "-8000"},
		"Cash":           {s.Cash.String(0), "87000"}, // рубли 50 000 + LQDT 30 000 + кеш Финама 7 000
		"PortfolioValue": {s.PortfolioValue.String(0), "1700000"},
		"DayChange":      {s.DayChange.String(0), "700"},
		"Total":          {s.Total.String(0), "1300000"},
	}
	for name, c := range checks {
		if c[0] != c[1] {
			t.Errorf("%s = %s, ожидалось %s", name, c[0], c[1])
		}
	}
	if len(s.Holdings) != 1 || s.Holdings[0].Value.String(0) != "700000" || s.Holdings[0].DayChange.String(0) != "-2400" {
		t.Errorf("акции = %+v", s.Holdings)
	}
	if len(s.RealtyHoldings) != 2 || s.RealtyHoldings[0].Ticker != "XACCSK" {
		t.Errorf("недвижимость = %+v", s.RealtyHoldings)
	}
	if strings.Join(s.Unclassified, ",") != "SU26238RMFS4,UNKNOWNFUND" {
		t.Errorf("вне классов = %v", s.Unclassified)
	}
}

func TestHoldingLabel(t *testing.T) {
	m := &Meta{Names: map[string]string{"SBER": "Сбербанк"}}
	cases := []struct {
		h          Holding
		ticker, nm string
	}{
		// Тикер остаётся кодом, короткое имя из справочника — названием.
		{Holding{Ticker: "RU000A105328", Name: "ЗПИФ Парус-ЛОГ"}, "RU000A105328", "Парус-Логистика"},
		{Holding{Ticker: "SBER", Name: "Сбер от брокера"}, "SBER", "Сбербанк"},
		{Holding{Ticker: "NEWFUND", Name: "Новый фонд"}, "NEWFUND", "Новый фонд"},
	}
	for _, c := range cases {
		if tk, nm := HoldingLabel(&c.h, m); tk != c.ticker || nm != c.nm {
			t.Errorf("HoldingLabel(%s) = %q, %q; ожидалось %q, %q", c.h.Ticker, tk, nm, c.ticker, c.nm)
		}
	}
}

func finamDec(t *testing.T, s string) finam.Decimal {
	t.Helper()
	return finam.Decimal{Dec: dec(t, s)}
}

// Счёт Финама переводится в общий вид: тип и название — из справки, пустые и
// нерублёвые позиции пропускаются, кеш счёта — отдельной суммой.
func TestFinamSourceAddAccount(t *testing.T) {
	f := NewFinamSource(nil)
	// Справка уже в кеше — в сеть источник не ходит.
	f.assets["SBER@MISX"] = &finam.Asset{Type: "EQUITIES", Name: "Сбербанк"}
	f.assets["XACCSK@MISX"] = &finam.Asset{Type: "FUNDS", Name: "ЗПИФ Акцент 5"}

	acc := &finam.Account{
		AccountID: "A1",
		Equity:    finamDec(t, "600000"),
		Cash: []finam.Money{
			{CurrencyCode: "RUB", Units: json.Number("5000")},
			{CurrencyCode: "USD", Units: json.Number("10")},
		},
		Positions: []finam.Position{
			{Symbol: "SBER@MISX", Quantity: finamDec(t, "100"), CurrentPrice: finamDec(t, "300"),
				UnrealizedPnL: finamDec(t, "1000"), DailyPnL: finamDec(t, "50")},
			{Symbol: "XACCSK@MISX", Quantity: finamDec(t, "344"), CurrentPrice: finamDec(t, "870"),
				UnrealizedPnL: finamDec(t, "-1720"), DailyPnL: finamDec(t, "-344")},
			{Symbol: "EMPTY@MISX", Quantity: finamDec(t, "0"), CurrentPrice: finamDec(t, "1")},
			{Symbol: "AAPL@XNGS", Quantity: finamDec(t, "1"), CurrentPrice: finamDec(t, "200"), CurrentPriceCurrency: "USD"},
		},
	}
	part := &SourcePortfolio{}
	if err := f.addAccount(t.Context(), acc, part); err != nil {
		t.Fatal(err)
	}

	if part.Value.String(0) != "600000" || part.Cash.String(0) != "5000" || part.DayChange.String(0) != "-294" {
		t.Errorf("итоги: value %s, cash %s, day %s", part.Value.String(0), part.Cash.String(0), part.DayChange.String(0))
	}
	if len(part.Positions) != 2 {
		t.Fatalf("позиций %d, ожидалось 2: %+v", len(part.Positions), part.Positions)
	}
	sber, fund := part.Positions[0], part.Positions[1]
	if sber.Ticker != "SBER" || sber.Kind != KindShare || sber.Name != "Сбербанк" || sber.Value.String(0) != "30000" {
		t.Errorf("акция = %+v", sber)
	}
	if fund.Ticker != "XACCSK" || fund.Kind != KindFund || fund.Value.String(0) != "299280" {
		t.Errorf("фонд = %+v", fund)
	}
	// Акция с Финама попадает в акции, фонд — в недвижимость.
	s := buildSnapshot(time.Now(), []*SourcePortfolio{part})
	if s.Shares.String(0) != "30000" || s.Realty.String(0) != "299280" {
		t.Errorf("классы: акции %s, недвижимость %s", s.Shares.String(0), s.Realty.String(0))
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
	again, err := applySnapshot(t.Context(), out, s, &Meta{})
	if err != nil {
		t.Fatal(err)
	}
	if again != out {
		t.Errorf("повторный прогон изменил таблицу:\n%s", again)
	}
}

// Справка Финама недоступна: бумагу, которую не узнать по тикеру, нельзя молча
// выкинуть «вне классов» — это ошибка источника. Известный по тикеру ЗПИФ
// справка не нужна.
func TestFinamSourceAssetFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/sessions", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"token":"jwt"}`)
	})
	mux.HandleFunc("GET /v1/assets/{symbol}", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"code":5,"message":"not found"}`, http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	f := NewFinamSource(finam.NewClient("s", finam.WithBaseURL(srv.URL)))

	acc := func(symbol string) *finam.Account {
		return &finam.Account{AccountID: "A1", Positions: []finam.Position{
			{Symbol: symbol, Quantity: finamDec(t, "1"), CurrentPrice: finamDec(t, "100")},
		}}
	}

	if err := f.addAccount(t.Context(), acc("SBER@MISX"), &SourcePortfolio{}); err == nil {
		t.Error("акция без справки: ожидалась ошибка источника")
	}

	part := &SourcePortfolio{}
	if err := f.addAccount(t.Context(), acc("XACCSK@MISX"), part); err != nil {
		t.Fatalf("ЗПИФ из справочника без справки: %v", err)
	}
	if s := buildSnapshot(time.Now(), []*SourcePortfolio{part}); s.Realty.String(0) != "100" {
		t.Errorf("недвижимость = %s, ожидалось 100", s.Realty.String(0))
	}
}
