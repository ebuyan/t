package portfolio

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"tinvest/internal/tinvest"
)

// roundStep — до какого шага округляется доходность в реестре.
const roundStep = 1000

// registryEntry — одна запись реестра в формате inline-полей Dataview.
type registryEntry struct {
	date   string
	income tinvest.Dec
	stock  tinvest.Dec
	gold   tinvest.Dec
}

func newRegistryEntry(s *Snapshot) registryEntry {
	stock := s.StockYield.CeilTo(roundStep)
	gold := s.GoldYield.CeilTo(roundStep)
	return registryEntry{
		date: s.Date.Format("2006-01-02"),
		// income считаем от уже округлённых слагаемых, иначе в файле
		// нарушится инвариант income = stock + gold.
		income: stock.Add(gold),
		stock:  stock,
		gold:   gold,
	}
}

func (e registryEntry) render() []string {
	return []string{
		"- date:: " + e.date,
		"  income:: " + e.income.String(0),
		"  stock:: " + e.stock.String(0),
		"  gold:: " + e.gold.String(0),
	}
}

// UpdateRegistryFile добавляет запись в начало реестра. Как и файл долей,
// пишется через атомарную замену — файл живёт в синкающемся волте.
func UpdateRegistryFile(ctx context.Context, path string, s *Snapshot) error {
	//nolint:gosec // путь берётся из доверенного env-конфига, не из пользовательского ввода
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	entry := newRegistryEntry(s)
	updated := upsertEntry(string(content), entry)
	if updated == string(content) {
		slog.InfoContext(ctx, "registry entry already up to date", slog.String("date", entry.date))
		return nil
	}

	slog.InfoContext(ctx, "registry updated",
		slog.String("date", entry.date),
		slog.String("income", entry.income.String(0)),
		slog.String("stock", entry.stock.String(0)),
		slog.String("gold", entry.gold.String(0)),
	)

	return writeAtomic(path, []byte(updated))
}

// upsertEntry вставляет запись в начало списка. Если запись за эту дату уже
// есть, она заменяется на месте — повторный запуск в тот же день не плодит
// дубли и не переупорядочивает файл.
func upsertEntry(content string, e registryEntry) string {
	lines := strings.Split(content, "\n")
	block := e.render()

	if at := entryIndex(lines, e.date); at >= 0 {
		end := entryEnd(lines, at)
		out := append([]string{}, lines[:at]...)
		out = append(out, block...)
		out = append(out, lines[end:]...)
		return strings.Join(out, "\n")
	}

	// Вставляем перед первой существующей записью, сохраняя всё, что стоит
	// выше неё (в файле это ведущая пустая строка).
	at := firstEntryIndex(lines)
	if at < 0 {
		at = len(lines)
	}
	out := append([]string{}, lines[:at]...)
	out = append(out, block...)
	out = append(out, "")
	out = append(out, lines[at:]...)
	return strings.Join(out, "\n")
}

const datePrefix = "- date:: "

func firstEntryIndex(lines []string) int {
	for i, l := range lines {
		if strings.HasPrefix(l, datePrefix) {
			return i
		}
	}
	return -1
}

func entryIndex(lines []string, date string) int {
	want := datePrefix + date
	for i, l := range lines {
		if strings.TrimRight(l, " \t") == want {
			return i
		}
	}
	return -1
}

// entryEnd возвращает индекс за последней строкой записи, начинающейся на at.
// Запись — это строка date и следующие за ней строки полей с отступом.
func entryEnd(lines []string, at int) int {
	i := at + 1
	for i < len(lines) {
		l := lines[i]
		if strings.TrimSpace(l) == "" || strings.HasPrefix(l, datePrefix) {
			break
		}
		i++
	}
	return i
}
