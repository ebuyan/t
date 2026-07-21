package portfolio

import (
	"strings"
)

// table — распарсенная markdown-таблица: шапка, строка-разделитель и данные.
type table struct {
	startLine int // индекс первой строки таблицы в исходном файле
	endLine   int // индекс за последней строкой
	header    []string
	rows      [][]string
}

// document — файл, разобранный на строки с найденными в нём таблицами.
type document struct {
	lines  []string
	tables []*table
}

func parseDocument(content string) *document {
	// Разбиваем так, чтобы склейка обратно давала исходный текст байт в байт.
	lines := strings.Split(content, "\n")
	doc := &document{lines: lines}

	for i := 0; i < len(lines); {
		if !isTableLine(lines[i]) {
			i++
			continue
		}
		start := i
		for i < len(lines) && isTableLine(lines[i]) {
			i++
		}
		// Таблица — минимум шапка и разделитель.
		if i-start < 2 || !isSeparatorRow(splitRow(lines[start+1])) {
			continue
		}
		t := &table{startLine: start, endLine: i, header: splitRow(lines[start])}
		for _, l := range lines[start+2 : i] {
			t.rows = append(t.rows, splitRow(l))
		}
		doc.tables = append(doc.tables, t)
	}
	return doc
}

func isTableLine(s string) bool {
	return strings.HasPrefix(strings.TrimSpace(s), "|")
}

// splitRow разбирает «| a | b |» в ["a", "b"].
func splitRow(line string) []string {
	s := strings.TrimSpace(line)
	s = strings.TrimPrefix(s, "|")
	s = strings.TrimSuffix(s, "|")
	parts := strings.Split(s, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func isSeparatorRow(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		c = strings.TrimSpace(c)
		if c == "" {
			return false
		}
		for _, r := range c {
			if r != '-' && r != ':' {
				return false
			}
		}
	}
	return true
}

// name возвращает заголовок первого столбца — по нему опознаём таблицу
// («Актив», «Компания», «Сектор», «Дивиденды»).
func (t *table) name() string {
	if len(t.header) == 0 {
		return ""
	}
	return t.header[0]
}

// columnIndex ищет столбец по заголовку.
func (t *table) columnIndex(header string) int {
	for i, h := range t.header {
		if h == header {
			return i
		}
	}
	return -1
}

// rowIndex ищет строку по ключу первого столбца.
func (t *table) rowIndex(key string) int {
	for i, r := range t.rows {
		if len(r) > 0 && r[0] == key {
			return i
		}
	}
	return -1
}

// setColumn проставляет значения в столбец header, создавая его при отсутствии.
// values — по ключу первого столбца; строки без значения получают missing.
// Новые ключи из order, которых ещё нет в таблице, дописываются в конец.
func (t *table) setColumn(header string, values map[string]string, order []string, missing string) {
	col := t.columnIndex(header)
	if col < 0 {
		t.header = append(t.header, header)
		col = len(t.header) - 1
	}

	// Новый актив: в прошлых столбцах его не было, ставим туда прочерк —
	// так же, как файл отмечает выбывшие позиции.
	for _, key := range order {
		if t.rowIndex(key) < 0 {
			row := make([]string, len(t.header))
			row[0] = key
			for i := 1; i < len(row); i++ {
				row[i] = missing
			}
			t.rows = append(t.rows, row)
		}
	}

	for i, r := range t.rows {
		// Выравниваем строку по ширине шапки: в исходном файле строки короче не
		// встречаются, но подстраховка дешевле разъехавшейся таблицы.
		for len(r) < len(t.header) {
			r = append(r, "")
		}
		key := r[0]
		if v, ok := values[key]; ok {
			r[col] = v
		} else if r[col] == "" {
			r[col] = missing
		}
		t.rows[i] = r
	}
}

// render собирает таблицу обратно, выравнивая столбцы по ширине — как в исходном файле.
func (t *table) render() []string {
	widths := make([]int, len(t.header))
	for i, h := range t.header {
		widths[i] = runeLen(h)
	}
	for _, r := range t.rows {
		for i, c := range r {
			if i < len(widths) && runeLen(c) > widths[i] {
				widths[i] = runeLen(c)
			}
		}
	}

	// шапка + разделитель + строки данных
	out := make([]string, 0, 2+len(t.rows))
	out = append(out, renderRow(t.header, widths))

	sep := make([]string, len(t.header))
	for i := range sep {
		sep[i] = strings.Repeat("-", widths[i])
	}
	out = append(out, renderRow(sep, widths))

	for _, r := range t.rows {
		out = append(out, renderRow(r, widths))
	}
	return out
}

func renderRow(cells []string, widths []int) string {
	var b strings.Builder
	b.WriteString("|")
	for i, w := range widths {
		c := ""
		if i < len(cells) {
			c = cells[i]
		}
		b.WriteString(" ")
		b.WriteString(c)
		b.WriteString(strings.Repeat(" ", w-runeLen(c)))
		b.WriteString(" |")
	}
	return b.String()
}

func runeLen(s string) int {
	return len([]rune(s))
}

// render собирает документ обратно, заменяя строки таблиц на перерисованные.
// Всё, что таблицами не является, сохраняется без изменений.
func (d *document) render() string {
	var out []string
	prev := 0
	for _, t := range d.tables {
		out = append(out, d.lines[prev:t.startLine]...)
		out = append(out, t.render()...)
		prev = t.endLine
	}
	out = append(out, d.lines[prev:]...)
	return strings.Join(out, "\n")
}
