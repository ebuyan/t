package portfolio

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"tinvest/internal/tinvest"
)

// Заголовки первых столбцов, по которым опознаём таблицы в файле.
const (
	tableAssets    = "Актив"
	tableCompanies = "Компания"
	tableSectors   = "Сектор"
	tableDividends = "Дивиденды"
)

// missing — чем заполняются строки, для которых в новом срезе нет значения
// (актив продан). Тот же символ, что уже стоит в файле.
const missing = "—"

// sectorNames — перевод sector из InstrumentsService в названия строк файла.
var sectorNames = map[string]string{
	"energy":       "Энергетика",
	"materials":    "Сырьевая промышленность",
	"financial":    "Финансовый сектор",
	"consumer":     "Потребительские товары",
	"utilities":    "Электроэнергетика",
	"health_care":  "Здравоохранение",
	"real_estate":  "Недвижимость",
	"it":           "Информационные технологии",
	"telecom":      "Телекоммуникации",
	"industrials":  "Промышленность",
	"ecomaterials": "Сырьевая промышленность",
}

// UpdatePortfolioFile дописывает в файл столбец с текущим срезом. Требуются и
// срез, и метаданные (названия/секторы/дивиденды). Файл живёт в синкающемся
// волте, поэтому запись идёт во временный файл рядом и атомарный rename —
// оборванная запись не оставит половину таблицы.
func UpdatePortfolioFile(ctx context.Context, path string, s *Snapshot, m *Meta) error {
	//nolint:gosec // путь берётся из доверенного env-конфига, не из пользовательского ввода
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	updated, err := applySnapshot(ctx, string(content), s, m)
	if err != nil {
		return err
	}
	if updated == string(content) {
		slog.InfoContext(ctx, "portfolio file already has this column, no changes")
		return nil
	}

	return writeAtomic(path, []byte(updated))
}

// applySnapshot добавляет столбец во все четыре таблицы документа.
func applySnapshot(ctx context.Context, content string, s *Snapshot, m *Meta) (string, error) {
	doc := parseDocument(content)
	if len(doc.tables) == 0 {
		return "", fmt.Errorf("no markdown table found in file")
	}

	col := s.ColumnDate()
	var touched []string

	for _, t := range doc.tables {
		switch t.name() {
		case tableAssets:
			values, order := assetValues(s)
			t.setColumn(col, values, order, missing)
			touched = append(touched, tableAssets)

		case tableCompanies:
			byTicker, labels, order := companyValues(s, m)
			values, rowOrder := alignByTicker(t, byTicker, labels, order)
			t.setColumn(col, values, rowOrder, missing)
			touched = append(touched, tableCompanies)

		case tableSectors:
			values, order := sectorValues(ctx, s, m)
			t.setColumn(col, values, order, missing)
			touched = append(touched, tableSectors)

		case tableDividends:
			byTicker, labels, order := dividendValues(s, m)
			values, rowOrder := alignByTicker(t, byTicker, labels, order)
			// Столбец дивидендов — год, а не дата: он обновляется на месте,
			// новый столбец каждый квартал здесь не нужен. Пустое значение
			// (дивидендов за год нет) оставляем пустым, как в файле.
			t.setColumn(s.Date.Format("2006"), values, rowOrder, "")
			touched = append(touched, tableDividends)
		}
	}

	if len(touched) == 0 {
		return "", fmt.Errorf("no known table found (%s, %s, %s, %s)",
			tableAssets, tableCompanies, tableSectors, tableDividends)
	}
	slog.InfoContext(ctx, "tables updated", slog.String("tables", strings.Join(touched, ", ")))

	return doc.render(), nil
}

// --- Значения столбцов из среза и метаданных ---

// assetValues — строки таблицы «Актив». База долей — акции + золото + кеш
// (ShareBase), поэтому Акции/Золото/Кеш в сумме дают 100%; «Всего» — эта же база
// в рублях (полная стоимость трёх классов).
func assetValues(s *Snapshot) (map[string]string, []string) {
	base := s.ShareBase()
	return map[string]string{
		"Акции":     pct(s.Shares.Percent(base)),
		"Золото":    pct(s.Gold.Percent(base)),
		"Кеш":       pct(s.Cash.Percent(base)),
		"**Всего**": rub(base),
	}, []string{"Акции", "Золото", "Кеш", "**Всего**"}
}

// companyValues — доли компаний от общей базы (акции + золото + кеш, как в таблице
// «Актив»). Ключ — тикер, подпись «TICKER — Название».
func companyValues(s *Snapshot, m *Meta) (byTicker, labels map[string]string, order []string) {
	base := s.ShareBase()
	byTicker = map[string]string{}
	labels = map[string]string{}
	for _, h := range s.Holdings {
		byTicker[h.Ticker] = pct(h.Value.Percent(base))
		labels[h.Ticker] = fmt.Sprintf("%s — %s", h.Ticker, m.Names[h.UID])
		order = append(order, h.Ticker)
	}
	return byTicker, labels, order
}

// sectorValues — доли секторов, нормированные к акциям (в сумме 100%).
func sectorValues(ctx context.Context, s *Snapshot, m *Meta) (map[string]string, []string) {
	sums := map[string]tinvest.Dec{}
	for _, h := range s.Holdings {
		sector := m.Sectors[h.UID]
		name, ok := sectorNames[sector]
		if !ok {
			name = sector
			slog.WarnContext(ctx, "unknown sector, keeping as is",
				slog.String("sector", sector), slog.String("ticker", h.Ticker))
		}
		sums[name] = sums[name].Add(h.Value)
	}

	values := map[string]string{}
	order := make([]string, 0, len(sums))
	for name, sum := range sums {
		values[name] = pct(sum.Percent(s.Shares))
		order = append(order, name)
	}
	sort.Slice(order, func(i, j int) bool {
		return sums[order[i]].Cmp(sums[order[j]]) > 0
	})
	return values, order
}

// dividendValues — дивидендная доходность за год, как её показывает Т-Банк.
func dividendValues(s *Snapshot, m *Meta) (byTicker, labels map[string]string, order []string) {
	byTicker = map[string]string{}
	labels = map[string]string{}
	for _, h := range s.Holdings {
		labels[h.Ticker] = fmt.Sprintf("%s — %s", h.Ticker, m.Names[h.UID])
		d, ok := m.Dividends[h.Ticker]
		if !ok {
			continue
		}
		byTicker[h.Ticker] = pct(d)
		order = append(order, h.Ticker)
	}
	return byTicker, labels, order
}

// alignByTicker сопоставляет значения по тикеру со строками таблицы, где первый
// столбец выглядит как «SBERP — Сбер Банк». Существующие подписи не трогаем: они
// могут быть отредактированы вручную. Для тикеров, которых в таблице ещё нет,
// ключом становится подпись из API.
func alignByTicker(t *table, byTicker, labels map[string]string, order []string) (map[string]string, []string) {
	values := map[string]string{}
	seen := map[string]bool{}

	for _, r := range t.rows {
		if len(r) == 0 {
			continue
		}
		ticker := tickerOf(r[0])
		if v, ok := byTicker[ticker]; ok {
			values[r[0]] = v
			seen[ticker] = true
		}
	}

	var rowOrder []string
	for _, ticker := range order {
		if seen[ticker] {
			continue
		}
		label := labels[ticker]
		values[label] = byTicker[ticker]
		rowOrder = append(rowOrder, label)
	}
	return values, rowOrder
}

// tickerOf вытаскивает тикер из подписи «SBERP — Сбер Банк».
func tickerOf(label string) string {
	if i := strings.Index(label, "—"); i >= 0 {
		return strings.TrimSpace(label[:i])
	}
	return strings.TrimSpace(label)
}

// writeAtomic пишет во временный файл в том же каталоге и переименовывает поверх.
// Тот же каталог обязателен: rename атомарен только в пределах файловой системы.
func writeAtomic(path string, content []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".tinvest-*.tmp")
	if err != nil {
		return fmt.Errorf("temp file in %s: %w", dir, err)
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }() // no-op после успешного rename

	if _, err := f.Write(content); err != nil {
		_ = f.Close()
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("sync %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmp, err)
	}
	//nolint:gosec // 0644 намеренно: итоговый файл в волте должен читаться другими инструментами
	if err := os.Chmod(tmp, 0o644); err != nil {
		return fmt.Errorf("chmod %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename %s: %w", path, err)
	}
	return nil
}
