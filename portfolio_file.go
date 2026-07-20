package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
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

// updatePortfolioFile дописывает в файл столбец с текущим срезом.
// Файл живёт в синкающемся волте, поэтому: сначала бэкап, потом запись во
// временный файл рядом и атомарный rename — оборванная запись не оставит
// половину таблицы.
func updatePortfolioFile(path string, s *snapshot) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("чтение %s: %w", path, err)
	}

	updated, err := applySnapshot(string(content), s)
	if err != nil {
		return err
	}
	if updated == string(content) {
		log.Print("файл портфеля уже содержит этот столбец — изменений нет")
		return nil
	}

	backup, err := writeBackup(path, content)
	if err != nil {
		return err
	}
	if backup != "" {
		log.Printf("бэкап: %s", backup)
	}

	return writeAtomic(path, []byte(updated))
}

// applySnapshot добавляет столбец во все четыре таблицы документа.
func applySnapshot(content string, s *snapshot) (string, error) {
	doc := parseDocument(content)
	if len(doc.tables) == 0 {
		return "", fmt.Errorf("в файле не найдено ни одной markdown-таблицы")
	}

	col := s.columnDate()
	var touched []string

	for _, t := range doc.tables {
		switch t.name() {
		case tableAssets:
			values, order := s.assetValues()
			t.setColumn(col, values, order, missing)
			touched = append(touched, tableAssets)

		case tableCompanies:
			byTicker, labels, order := s.companyValues()
			values, rowOrder := alignByTicker(t, byTicker, labels, order)
			t.setColumn(col, values, rowOrder, missing)
			touched = append(touched, tableCompanies)

		case tableSectors:
			values, order := s.sectorValues()
			t.setColumn(col, values, order, missing)
			touched = append(touched, tableSectors)

		case tableDividends:
			byTicker, labels, order := s.dividendValues()
			values, rowOrder := alignByTicker(t, byTicker, labels, order)
			// Столбец дивидендов — год, а не дата: он обновляется на месте,
			// новый столбец каждый квартал здесь не нужен. Пустое значение
			// (дивидендов за год нет) оставляем пустым, как в файле.
			t.setColumn(s.date.Format("2006"), values, rowOrder, "")
			touched = append(touched, tableDividends)
		}
	}

	if len(touched) == 0 {
		return "", fmt.Errorf("не найдено ни одной знакомой таблицы (%s, %s, %s, %s)",
			tableAssets, tableCompanies, tableSectors, tableDividends)
	}
	log.Printf("обновлены таблицы: %s", strings.Join(touched, ", "))

	return doc.render(), nil
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

// writeBackup кладёт копию рядом с файлом: <имя>.bak-<дата>.
// При TINVEST_BACKUP=false ничего не делает и возвращает пустой путь.
func writeBackup(path string, content []byte) (string, error) {
	if !cfg.Backup {
		return "", nil
	}
	backup := fmt.Sprintf("%s.bak-%s", path, time.Now().Format("2006-01-02T15-04-05"))
	if err := os.WriteFile(backup, content, 0o644); err != nil {
		return "", fmt.Errorf("бэкап %s: %w", backup, err)
	}
	return backup, nil
}

// writeAtomic пишет во временный файл в том же каталоге и переименовывает поверх.
// Тот же каталог обязателен: rename атомарен только в пределах файловой системы.
func writeAtomic(path string, content []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".tportfolio-*.tmp")
	if err != nil {
		return fmt.Errorf("временный файл в %s: %w", dir, err)
	}
	tmp := f.Name()
	defer os.Remove(tmp) // no-op после успешного rename

	if _, err := f.Write(content); err != nil {
		f.Close()
		return fmt.Errorf("запись %s: %w", tmp, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("sync %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("закрытие %s: %w", tmp, err)
	}
	if err := os.Chmod(tmp, 0o644); err != nil {
		return fmt.Errorf("права на %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("замена %s: %w", path, err)
	}
	return nil
}
