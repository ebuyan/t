package tinvest

import (
	"encoding/json"
	"testing"
)

// Ответ GetOperationsByCursor: тип операции лежит в поле type, суммы приходят
// строкой в units, у налога payment отрицательный.
func TestOperationUnmarshal(t *testing.T) {
	const raw = `{
		"id": "1", "type": "OPERATION_TYPE_DIVIDEND", "ticker": "LKOH",
		"date": "2026-05-20T10:00:00Z",
		"payment": {"currency": "rub", "units": "1500", "nano": 500000000}
	}`

	var op Operation
	if err := json.Unmarshal([]byte(raw), &op); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if op.Type != opTypeDividend {
		t.Errorf("Type = %q, хотим %q", op.Type, opTypeDividend)
	}
	if got := op.Payment.Dec().String(2); got != "1500.50" {
		t.Errorf("payment = %q, хотим %q", got, "1500.50")
	}
	if op.Date.Year() != 2026 {
		t.Errorf("Date = %v, хотим 2026 год", op.Date)
	}
}

func rubOp(opType, ticker string, units int64, nano int32) Operation {
	return Operation{
		Type:    opType,
		Ticker:  ticker,
		Payment: MoneyValue{Currency: "rub", Units: jsonNum(units), Nano: nano},
	}
}

func TestSumDividends(t *testing.T) {
	ops := []Operation{
		rubOp(opTypeDividend, "LKOH", 10000, 0),
		rubOp(opTypeDividendTax, "LKOH", -1300, 0),
		rubOp(opTypeDividend, "SBER", 5000, 0),
		rubOp(opTypeDividendTaxProgressive, "SBER", -750, 0),
		rubOp(opTypeTaxCorrectionDividend, "SBER", 50, 0),
		rubOp(opTypeDivExt, "MTSS", 2000, 0),
		{Type: opTypeDividend, Ticker: "TSLA", Payment: MoneyValue{Currency: "usd", Units: 7}},
	}

	d := SumDividends(ops)

	// 10000 − 1300 + 5000 − 750 + 50; выплата на карту и валютная — мимо суммы.
	if got := d.Net.String(2); got != "13000.00" {
		t.Errorf("Net = %q, хотим %q", got, "13000.00")
	}
	if got := d.ToCard.String(2); got != "2000.00" {
		t.Errorf("ToCard = %q, хотим %q", got, "2000.00")
	}
	// Разбивка по тикеру — только то, что вошло в Net.
	if len(d.ByTicker) != 2 || d.ByTicker["LKOH"].String(2) != "8700.00" || d.ByTicker["SBER"].String(2) != "4300.00" {
		t.Errorf("ByTicker = %v", d.ByTicker)
	}
	if len(d.Skipped) != 1 || d.Skipped[0] != "TSLA" {
		t.Errorf("Skipped = %v, хотим [TSLA]", d.Skipped)
	}
}

// Чужие типы операций в сумму не попадают: фильтр на стороне API, но полагаться
// только на него нельзя.
func TestSumDividendsIgnoresOtherTypes(t *testing.T) {
	ops := []Operation{
		rubOp(opTypeDividend, "LKOH", 1000, 0),
		rubOp("OPERATION_TYPE_BUY", "LKOH", -50000, 0),
		rubOp("OPERATION_TYPE_COUPON", "OFZ", 300, 0),
	}
	if got := SumDividends(ops).Net.String(2); got != "1000.00" {
		t.Errorf("Net = %q, хотим %q", got, "1000.00")
	}
}

// В запрос идут все дивидендные типы плюс выплата на карту — иначе о ней нечего
// было бы предупредить.
func TestDividendOperationTypes(t *testing.T) {
	types := DividendOperationTypes()
	if len(types) != len(dividendNetTypes)+1 {
		t.Fatalf("типов %d, хотим %d", len(types), len(dividendNetTypes)+1)
	}
	if types[len(types)-1] != opTypeDivExt {
		t.Errorf("последний тип %q, хотим %q", types[len(types)-1], opTypeDivExt)
	}
	// Возвращаем копию: вызывающий не должен испортить общий список.
	types[0] = "MUTATED"
	if dividendNetTypes[0] == "MUTATED" {
		t.Error("DividendOperationTypes отдал ссылку на общий слайс")
	}
}
