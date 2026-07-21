package tinvest

import (
	"encoding/json"
	"testing"
)

func TestDecString(t *testing.T) {
	tests := []struct {
		name string
		q    Quotation
		prec int
		want string
	}{
		{"целое", Quotation{Units: 12, Nano: 0}, 2, "12.00"},
		{"дробное", Quotation{Units: 12, Nano: 340000000}, 2, "12.34"},
		{"округление вверх", Quotation{Units: 0, Nano: 125000000}, 2, "0.13"},
		{"округление вниз", Quotation{Units: 0, Nano: 124000000}, 2, "0.12"},
		{"отрицательное", Quotation{Units: -3, Nano: -500000000}, 2, "-3.50"},
		{"отрицательное дробное", Quotation{Units: 0, Nano: -450000000}, 2, "-0.45"},
		{"перенос в целое", Quotation{Units: 1, Nano: 999000000}, 2, "2.00"},
		{"без дробной части", Quotation{Units: 1234, Nano: 560000000}, 0, "1235"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.q.Dec().String(tt.prec); got != tt.want {
				t.Errorf("String(%d) = %q, хотим %q", tt.prec, got, tt.want)
			}
		})
	}
}

// REST-обёртка отдаёт int64 строкой, а gRPC-совместимые клиенты — числом.
func TestJSONNumAcceptsStringAndNumber(t *testing.T) {
	for _, raw := range []string{
		`{"currency":"rub","units":"1234","nano":560000000}`,
		`{"currency":"rub","units":1234,"nano":560000000}`,
	} {
		var m MoneyValue
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatalf("Unmarshal(%s): %v", raw, err)
		}
		if got := m.Dec().String(2); got != "1234.56" {
			t.Errorf("Unmarshal(%s) → %q, хотим \"1234.56\"", raw, got)
		}
	}
}
func TestTotalYield(t *testing.T) {
	p := &Portfolio{
		TotalAmountPortfolio: MoneyValue{Currency: "rub", Units: 100},
		Positions: []Position{
			{Ticker: "SBERP", CurrentPrice: MoneyValue{Currency: "rub"}, ExpectedYield: Quotation{Units: -83178, Nano: -730000000}},
			{Ticker: "ROSN", CurrentPrice: MoneyValue{Currency: "rub"}, ExpectedYield: Quotation{Units: -120557, Nano: -800000000}},
		},
	}
	total, skipped := p.TotalYield()
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, хотим пусто", skipped)
	}
	if got, want := total.String(2), "-203736.53"; got != want {
		t.Errorf("TotalYield = %s, хотим %s", got, want)
	}
}

func TestGroup(t *testing.T) {
	tests := map[string]string{
		"1234567.89": "1 234 567.89",
		"-1234.00":   "-1 234.00",
		"999.99":     "999.99",
	}
	for in, want := range tests {
		if got := Group(in); got != want {
			t.Errorf("Group(%q) = %q, хотим %q", in, got, want)
		}
	}
}

// Округление вверх до тысячи: к нулю для отрицательных, от нуля для положительных.
func TestCeilTo(t *testing.T) {
	tests := []struct {
		nanos int64
		want  string
	}{
		{-1162021 * nanoScale, "-1162000"},
		{-97248 * nanoScale, "-97000"},
		{1162021 * nanoScale, "1163000"},
		{-1162000 * nanoScale, "-1162000"},
		{1162000 * nanoScale, "1162000"},
		{0, "0"},
		{-1 * nanoScale, "0"},
	}
	for _, tt := range tests {
		if got := (Dec{nanos: tt.nanos}).CeilTo(1000).String(0); got != tt.want {
			t.Errorf("CeilTo(1000) для %d = %s, хотим %s", tt.nanos/nanoScale, got, tt.want)
		}
	}
}
