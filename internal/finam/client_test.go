package finam

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Ответ GetAccount в формате REST-шлюза: snake_case, числа строками.
const accountJSON = `{
  "account_id": "A1",
  "type": "UNION",
  "status": "ACCOUNT_ACTIVE",
  "equity": {"value": "301234.5"},
  "unrealized_profit": {"value": "-1200"},
  "positions": [
    {"symbol": "RU000A1034U7@MISX", "quantity": {"value": "344"}, "average_price": {"value": "875"},
     "current_price": {"value": "870.5"}, "daily_pnl": {"value": "-172"}, "unrealized_pnl": {"value": "-1548"},
     "current_price_currency": "RUB"}
  ],
  "cash": [{"currency_code": "RUB", "units": "1777", "nanos": 500000000}],
  "open_account_date": "2026-09-01T00:00:00Z"
}`

// fakeAPI — httptest-сервер с минимальным контрактом Финама. rejectFirst —
// сколько первых авторизованных запросов отвергнуть с 401 (протухший JWT).
type fakeAPI struct {
	auths       atomic.Int32
	rejectFirst atomic.Int32
	lastQuery   string
}

func (f *fakeAPI) handler(t *testing.T) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/sessions", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("Auth не должен получать заголовок Authorization")
		}
		var req struct{ Secret string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Secret != "s3cret" {
			http.Error(w, `{"code":16,"message":"bad secret"}`, http.StatusUnauthorized)
			return
		}
		n := f.auths.Add(1)
		_, _ = fmt.Fprintf(w, `{"token":"jwt-%d"}`, n)
	})
	mux.HandleFunc("POST /v1/sessions/details", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("TokenDetails не должен получать заголовок Authorization")
		}
		_, _ = io.WriteString(w, `{"account_ids":["A1"],"readonly":true}`)
	})
	authed := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.Header.Get("Authorization"), "jwt-") {
				t.Errorf("Authorization = %q, ожидался JWT без Bearer", r.Header.Get("Authorization"))
			}
			if f.rejectFirst.Load() > 0 {
				f.rejectFirst.Add(-1)
				http.Error(w, `{"code":16,"message":"token expired"}`, http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}
	mux.HandleFunc("GET /v1/accounts/A1", authed(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, accountJSON)
	}))
	mux.HandleFunc("GET /v1/accounts/A1/transactions", authed(func(w http.ResponseWriter, r *http.Request) {
		f.lastQuery = r.URL.RawQuery
		_, _ = io.WriteString(w, `{"transactions":[
		  {"id":"1","symbol":"RU000A1034U7@MISX","transaction_category":"INCOME","change":{"currency_code":"RUB","units":"4200"}},
		  {"id":"2","symbol":"RU000A1034U7@MISX","transaction_category":8,"change":{"currency_code":"RUB","units":"-546"}}
		]}`)
	}))
	mux.HandleFunc("GET /v1/assets/RU000A1034U7@MISX", authed(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("account_id") != "A1" {
			t.Errorf("account_id = %q, ожидался A1", r.URL.Query().Get("account_id"))
		}
		_, _ = io.WriteString(w, `{"ticker":"RU000A1034U7","name":"ЗПИФ Акцент 5","type":"FUNDS"}`)
	}))
	return mux
}

func newTestClient(t *testing.T) (*Client, *fakeAPI) {
	t.Helper()
	f := &fakeAPI{}
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	return NewClient("s3cret", WithBaseURL(srv.URL)), f
}

func TestGetAccount(t *testing.T) {
	c, f := newTestClient(t)

	ids, err := c.AccountIDs(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "A1" {
		t.Fatalf("AccountIDs = %v, ожидалось [A1]", ids)
	}

	acc, err := c.GetAccount(t.Context(), "A1")
	if err != nil {
		t.Fatal(err)
	}
	if got := acc.Equity.String(2); got != "301234.50" {
		t.Errorf("Equity = %s", got)
	}
	if len(acc.Positions) != 1 {
		t.Fatalf("позиций %d, ожидалась 1", len(acc.Positions))
	}
	p := acc.Positions[0]
	if p.Ticker() != "RU000A1034U7" || !p.IsRUB() {
		t.Errorf("позиция: тикер %q, рубли %v", p.Ticker(), p.IsRUB())
	}
	if got := p.Quantity.Mul(p.CurrentPrice.Dec).String(2); got != "299452.00" {
		t.Errorf("стоимость позиции = %s, ожидалось 299452.00", got)
	}
	cash, err := acc.Cash[0].Dec()
	if err != nil || cash.String(2) != "1777.50" {
		t.Errorf("кеш = %s (%v), ожидалось 1777.50", cash.String(2), err)
	}
	if !acc.OpenAccountDate.Equal(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("OpenAccountDate = %v", acc.OpenAccountDate)
	}
	// JWT переиспользуется: один обмен секрета на оба запроса.
	if n := f.auths.Load(); n != 1 {
		t.Errorf("обменов секрета %d, ожидался 1", n)
	}
}

// Отвергнутый JWT перевыпускается, запрос повторяется и проходит.
func TestReissueTokenOn401(t *testing.T) {
	c, f := newTestClient(t)
	if _, err := c.token(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.rejectFirst.Store(1)

	if _, err := c.GetAccount(t.Context(), "A1"); err != nil {
		t.Fatalf("после перевыпуска JWT запрос должен пройти: %v", err)
	}
	if n := f.auths.Load(); n != 2 {
		t.Errorf("обменов секрета %d, ожидалось 2", n)
	}
}

func TestBadSecret(t *testing.T) {
	c, _ := newTestClient(t)
	c.secret = "wrong"
	_, err := c.GetAccount(t.Context(), "A1")
	if err == nil || !strings.Contains(err.Error(), "bad secret") {
		t.Fatalf("ожидалась ошибка авторизации с описанием API, получено %v", err)
	}
	// Секрет не должен утекать в текст ошибки.
	if strings.Contains(err.Error(), "wrong") {
		t.Errorf("секрет в тексте ошибки: %v", err)
	}
}

func TestTransactionsAndPayouts(t *testing.T) {
	c, f := newTestClient(t)
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	txs, err := c.Transactions(t.Context(), "A1", from, from.AddDate(0, 1, 0), 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"interval.start_time=2026-09-01T00%3A00%3A00Z", "interval.end_time=2026-10-01", "limit=1000"} {
		if !strings.Contains(f.lastQuery, want) {
			t.Errorf("в запросе %q нет %q", f.lastQuery, want)
		}
	}
	// Категория пришла и строкой, и числом.
	if txs[0].Category != CategoryIncome || txs[1].Category != CategoryTax {
		t.Errorf("категории: %q, %q", txs[0].Category, txs[1].Category)
	}
	if got := SumPayouts(txs).Net.String(2); got != "3654.00" {
		t.Errorf("выплаты = %s, ожидалось 3654.00", got)
	}
}

func TestGetAsset(t *testing.T) {
	c, _ := newTestClient(t)
	a, err := c.GetAsset(t.Context(), "RU000A1034U7@MISX", "A1")
	if err != nil {
		t.Fatal(err)
	}
	if a.Name != "ЗПИФ Акцент 5" {
		t.Errorf("Name = %q", a.Name)
	}
}

func TestSumPayouts(t *testing.T) {
	rub := func(units string) Money { return Money{CurrencyCode: "RUB", Units: json.Number(units)} }
	txs := []Transaction{
		{Symbol: "FUND@MISX", Category: CategoryIncome, Change: rub("1000")},
		{Symbol: "FUND@MISX", Category: CategoryTax, Change: rub("-130")},
		// Налог с продажи бумаги без выплат — не выплата.
		{Symbol: "SOLD@MISX", Category: CategoryTax, Change: rub("-500")},
		// Доход без бумаги (процент на остаток) — не выплата по позиции.
		{Category: CategoryIncome, Change: rub("77")},
		// Пополнение — не выплата.
		{Category: "DEPOSIT", Change: rub("100000")},
		// Выплата не в рублях — в пропущенные.
		{Symbol: "USD@XNYS", Category: CategoryIncome, Change: Money{CurrencyCode: "USD", Units: "5"}},
	}
	p := SumPayouts(txs)
	if got := p.Net.String(2); got != "870.00" {
		t.Errorf("Net = %s, ожидалось 870.00", got)
	}
	if len(p.Skipped) != 1 || p.Skipped[0] != "USD@XNYS" {
		t.Errorf("Skipped = %v", p.Skipped)
	}
}
