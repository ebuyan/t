package main

import (
	"strings"
	"testing"
	"time"
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

func entry(date string, stock, gold int64) registryEntry {
	s := stock + gold
	return registryEntry{
		date:   date,
		income: Dec{nanos: s * nanoScale},
		stock:  Dec{nanos: stock * nanoScale},
		gold:   Dec{nanos: gold * nanoScale},
	}
}

func TestUpsertEntryPrepends(t *testing.T) {
	got := upsertEntry(sampleRegistry, entry("2026-07-20", -1162000, -97000))

	want := `
- date:: 2026-07-20
  income:: -1259000
  stock:: -1162000
  gold:: -97000

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
	once := upsertEntry(sampleRegistry, entry("2026-07-20", -1162000, -97000))
	twice := upsertEntry(once, entry("2026-07-20", -1170000, -98000))

	if n := strings.Count(twice, "- date:: 2026-07-20"); n != 1 {
		t.Errorf("записей за 2026-07-20: %d, хотим 1", n)
	}
	if !strings.Contains(twice, "stock:: -1170000") {
		t.Error("значение не обновилось")
	}
	if strings.Contains(twice, "stock:: -1162000") {
		t.Error("осталось старое значение")
	}
	if n := strings.Count(twice, "- date::"); n != 3 {
		t.Errorf("всего записей: %d, хотим 3", n)
	}
}

// income должен сходиться с суммой округлённых слагаемых, иначе в файле
// появится запись, где income != stock + gold.
func TestRegistryEntryIncomeMatchesParts(t *testing.T) {
	s := &snapshot{
		date:       time.Date(2026, 7, 20, 11, 0, 0, 0, time.UTC),
		stockYield: Dec{nanos: -1162021 * nanoScale},
		goldYield:  Dec{nanos: -97248 * nanoScale},
	}
	e := newRegistryEntry(s)

	if got, want := e.stock.String(0), "-1162000"; got != want {
		t.Errorf("stock = %s, хотим %s", got, want)
	}
	if got, want := e.gold.String(0), "-97000"; got != want {
		t.Errorf("gold = %s, хотим %s", got, want)
	}
	if got, want := e.income.String(0), "-1259000"; got != want {
		t.Errorf("income = %s, хотим %s (сумма округлённых частей)", got, want)
	}
}

func TestUpsertEntryIntoEmptyFile(t *testing.T) {
	got := upsertEntry("", entry("2026-07-20", -1000, -2000))
	if !strings.Contains(got, "- date:: 2026-07-20") {
		t.Errorf("запись не добавлена в пустой файл: %q", got)
	}
}
