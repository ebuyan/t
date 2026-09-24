package portfolio

import (
	"strings"
	"testing"
	"time"

	"tinvest/internal/tinvest"
)

const sampleRegistry = `
- date:: 2026-07-18
  income:: -1300000
  stock:: -1200000
  gold:: -100000

- date:: 2026-07-17
  income:: -1200000
  stock:: -1100000
  gold:: -100000
`

func entry(date string, stock, gold, dividends int64) registryEntry {
	return registryEntry{
		date:      date,
		income:    tinvest.DecUnits(stock + gold + dividends),
		stock:     tinvest.DecUnits(stock),
		gold:      tinvest.DecUnits(gold),
		dividends: tinvest.DecUnits(dividends),
	}
}

func TestUpsertEntryPrepends(t *testing.T) {
	got := upsertEntry(sampleRegistry, entry("2026-07-20", -1162000, -97000, 200000))

	want := `
- date:: 2026-07-20
  income:: -1059000
  stock:: -1162000
  gold:: -97000
  dividends:: 200000
  realty:: 0

- date:: 2026-07-18`
	if !strings.HasPrefix(got, want) {
		t.Errorf("запись добавлена не в начало:\n%s", got[:200])
	}
	// Старые записи должны уцелеть.
	if !strings.Contains(got, "- date:: 2026-07-17") {
		t.Error("потеряна старая запись")
	}
	if n := strings.Count(got, "- date::"); n != 3 {
		t.Errorf("записей в файле: %d, хотим 3", n)
	}
}

// Повторный запуск в тот же день обновляет запись на месте, а не добавляет вторую.
func TestUpsertEntryReplacesSameDate(t *testing.T) {
	once := upsertEntry(sampleRegistry, entry("2026-07-20", -1162000, -97000, 200000))
	twice := upsertEntry(once, entry("2026-07-20", -1170000, -98000, 201000))

	if n := strings.Count(twice, "- date:: 2026-07-20"); n != 1 {
		t.Errorf("записей за 2026-07-20: %d, хотим 1", n)
	}
	if !strings.Contains(twice, "stock:: -1170000") {
		t.Error("значение не обновилось")
	}
	if strings.Contains(twice, "stock:: -1162000") {
		t.Error("осталось старое значение")
	}
	if !strings.Contains(twice, "dividends:: 201000") {
		t.Error("не обновились дивиденды")
	}
	if n := strings.Count(twice, "- date::"); n != 3 {
		t.Errorf("всего записей: %d, хотим 3", n)
	}
}

// income должен сходиться с суммой округлённых слагаемых, иначе в файле
// появится запись, где income != stock + gold + dividends + realty.
func TestRegistryEntryIncomeMatchesParts(t *testing.T) {
	s := &Snapshot{
		Date:        time.Date(2026, 7, 20, 11, 0, 0, 0, time.UTC),
		StockYield:  tinvest.DecUnits(-1162021),
		GoldYield:   tinvest.DecUnits(-97248),
		RealtyYield: tinvest.DecUnits(-15100),
	}
	e := newRegistryEntry(s, tinvest.DecUnits(200294))

	if got, want := e.stock.String(0), "-1162000"; got != want {
		t.Errorf("stock = %s, хотим %s", got, want)
	}
	if got, want := e.gold.String(0), "-97000"; got != want {
		t.Errorf("gold = %s, хотим %s", got, want)
	}
	if got, want := e.dividends.String(0), "201000"; got != want {
		t.Errorf("dividends = %s, хотим %s", got, want)
	}
	if got, want := e.realty.String(0), "-15000"; got != want {
		t.Errorf("realty = %s, хотим %s", got, want)
	}
	if got, want := e.income.String(0), "-1073000"; got != want {
		t.Errorf("income = %s, хотим %s (сумма округлённых частей)", got, want)
	}
}

// Порядок строк — контракт с dataviewjs в волте: он разбирает блок текстом по
// номерам строк, поэтому новые поля (dividends, realty) обязаны идти после gold.
func TestRegistryEntryFieldOrder(t *testing.T) {
	lines := entry("2026-07-20", -1162000, -97000, 200000).render()
	want := []string{"- date:: ", "  income:: ", "  stock:: ", "  gold:: ", "  dividends:: ", "  realty:: "}
	if len(lines) != len(want) {
		t.Fatalf("строк в записи: %d, хотим %d", len(lines), len(want))
	}
	for i, prefix := range want {
		if !strings.HasPrefix(lines[i], prefix) {
			t.Errorf("строка %d = %q, хотим префикс %q", i, lines[i], prefix)
		}
	}
}

func TestUpsertEntryIntoEmptyFile(t *testing.T) {
	got := upsertEntry("", entry("2026-07-20", -1000, -2000, 3000))
	if !strings.Contains(got, "- date:: 2026-07-20") {
		t.Errorf("запись не добавлена в пустой файл: %q", got)
	}
}
