package portfolio

import (
	"strings"
	"testing"
)

const sampleDoc = `
| Актив     | 2025.11.10 | 2026.03.27 |
| --------- | ---------- | ---------- |
| Акции     | ~100%      | 76,88%     |
| Золото    | 0%         | 23,12%     |
| **Всего** | 1 300 000  | 3 961 286  |

Произвольный текст между таблицами.

| Компания         | 2026.03.27 |
| ---------------- | ---------- |
| SBERP — Сбербанк | 7,92%      |
| LKOH — Лукойл    | —          |
`

// Документ без изменений должен пересобираться байт в байт, иначе любая правка
// файла в волте будет тащить за собой лишний diff.
func TestDocumentRenderIsStable(t *testing.T) {
	doc := parseDocument(sampleDoc)
	if len(doc.tables) != 2 {
		t.Fatalf("найдено таблиц: %d, хотим 2", len(doc.tables))
	}
	if got := doc.render(); got != sampleDoc {
		t.Errorf("render изменил документ:\n%q\nхотим\n%q", got, sampleDoc)
	}
}

func TestParseDocumentSkipsNonTables(t *testing.T) {
	doc := parseDocument("| просто строка с трубой\nтекст\n")
	if len(doc.tables) != 0 {
		t.Errorf("таблиц: %d, хотим 0 — без разделителя это не таблица", len(doc.tables))
	}
}

func TestSetColumnAddsAndFillsMissing(t *testing.T) {
	doc := parseDocument(sampleDoc)
	assets := doc.tables[0]

	assets.setColumn("2026.07.20", map[string]string{
		"Акции":     "76,02%",
		"**Всего**": "4 197 945",
	}, []string{"Акции", "**Всего**"}, "—")

	out := strings.Join(assets.render(), "\n")
	for _, want := range []string{"2026.07.20", "76,02%", "4 197 945"} {
		if !strings.Contains(out, want) {
			t.Errorf("в выводе нет %q:\n%s", want, out)
		}
	}
	// У «Золота» значения нет — должен появиться прочерк, а не пустая ячейка.
	gold := assets.rows[assets.rowIndex("Золото")]
	if last := gold[len(gold)-1]; last != "—" {
		t.Errorf("последняя ячейка золота = %q, хотим %q", last, "—")
	}
}

// Повторный запуск в тот же день не должен плодить столбцы-дубли.
func TestSetColumnIsIdempotent(t *testing.T) {
	doc := parseDocument(sampleDoc)
	assets := doc.tables[0]
	values := map[string]string{"Акции": "76,02%"}

	assets.setColumn("2026.07.20", values, []string{"Акции"}, "—")
	first := strings.Join(assets.render(), "\n")
	assets.setColumn("2026.07.20", values, []string{"Акции"}, "—")
	second := strings.Join(assets.render(), "\n")

	if first != second {
		t.Errorf("повторный setColumn изменил таблицу:\n%s\n---\n%s", first, second)
	}
	if n := strings.Count(assets.render()[0], "2026.07.20"); n != 1 {
		t.Errorf("столбец 2026.07.20 встречается %d раз, хотим 1", n)
	}
}

// Существующая подпись строки правится вручную — её нельзя перезаписывать
// названием из API, но значение в новый столбец попасть должно.
func TestAlignByTickerKeepsExistingLabels(t *testing.T) {
	doc := parseDocument(sampleDoc)
	companies := doc.tables[1]

	byTicker := map[string]string{"SBERP": "9,03%", "PHOR": "10,10%"}
	labels := map[string]string{
		"SBERP": "SBERP — Сбер Банк - привилегированные акции",
		"PHOR":  "PHOR — ФосАгро",
	}
	values, order := alignByTicker(companies, byTicker, labels, []string{"SBERP", "PHOR"})

	if got := values["SBERP — Сбербанк"]; got != "9,03%" {
		t.Errorf("значение по существующей подписи = %q, хотим 9,03%%", got)
	}
	if _, exists := values[labels["SBERP"]]; exists {
		t.Error("для SBERP добавлена вторая строка с названием из API")
	}
	if len(order) != 1 || order[0] != labels["PHOR"] {
		t.Errorf("новые строки = %v, хотим только PHOR", order)
	}
}

func TestTickerOf(t *testing.T) {
	tests := map[string]string{
		"SBERP — Сбер Банк":             "SBERP",
		"LSNGP — Россети Ленэнерго (п)": "LSNGP",
		"Акции":     "Акции",
		"**Всего**": "**Всего**",
	}
	for in, want := range tests {
		if got := tickerOf(in); got != want {
			t.Errorf("tickerOf(%q) = %q, хотим %q", in, got, want)
		}
	}
}

// Ключевая гарантия: текст вне таблиц не должен трогаться.
func TestApplyKeepsSurroundingText(t *testing.T) {
	doc := parseDocument(sampleDoc)
	doc.tables[0].setColumn("2026.07.20", map[string]string{"Акции": "76,02%"}, nil, "—")
	out := doc.render()

	if !strings.Contains(out, "Произвольный текст между таблицами.") {
		t.Error("потерян текст между таблицами")
	}
	if !strings.HasPrefix(out, "\n| Актив") {
		t.Errorf("изменено начало документа: %q", out[:20])
	}
}
