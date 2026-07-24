package portfolio

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"tportfolio/internal/tinvest"
)

// Обновление блоков ```progressbar``` в файле долей. Бар — это прогресс к цели:
// value = накоплено ÷ цель × 100 (в процентах), max всегда 100. Цель хранится в
// самом блоке новым полем goal (в тысячах рублей) и правится вручную в Obsidian.
// Обновляются только блоки kind: manual с известным по name классом; всё
// остальное в файле (в т.ч. блоки day-year и текст вне блоков) сохраняется
// байт-в-байт.

const (
	pbFence   = "```progressbar"
	fenceEnd  = "```"
	pbBarMax  = "100"
	goalScale = 1000 // goal задаётся в тысячах рублей
)

// barSource возвращает величину среза, от которой считается прогресс бара, по его
// имени (name в блоке). Второй результат — известен ли бар.
func barSource(name string, s *Snapshot) (tinvest.Dec, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "total":
		return s.PortfolioValue, true
	case "stoc", "stock":
		return s.Shares, true
	case "gold":
		return s.Gold, true
	case "reits":
		// Отдельного класса недвижимости в портфеле пока нет — прогресс 0%.
		return tinvest.Dec{}, true
	}
	return tinvest.Dec{}, false
}

// UpdateProgressBarsFile пересчитывает value в барах прогресса и переписывает файл.
// Как и таблицы долей, пишется через атомарную замену.
func UpdateProgressBarsFile(ctx context.Context, path string, s *Snapshot) error {
	//nolint:gosec // путь берётся из доверенного env-конфига, не из пользовательского ввода
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	updated, n := applyProgressBars(ctx, string(content), s)
	if updated == string(content) {
		slog.InfoContext(ctx, "progress bars already up to date")
		return nil
	}

	slog.InfoContext(ctx, "progress bars updated", slog.Int("bars", n))
	return writeAtomic(path, []byte(updated))
}

// applyProgressBars проходит по всем блокам ```progressbar``` и возвращает
// обновлённый текст и число изменённых баров. Блоки без закрывающего ограждения
// и весь текст вне блоков не трогаются.
func applyProgressBars(ctx context.Context, content string, s *Snapshot) (string, int) {
	lines := strings.Split(content, "\n")
	out := make([]string, 0, len(lines))
	changed := 0

	i := 0
	for i < len(lines) {
		if strings.TrimSpace(lines[i]) != pbFence {
			out = append(out, lines[i])
			i++
			continue
		}

		// Ищем закрывающее ограждение.
		j := i + 1
		for j < len(lines) && strings.TrimSpace(lines[j]) != fenceEnd {
			j++
		}
		if j >= len(lines) {
			// Блок не закрыт — оставляем как есть.
			out = append(out, lines[i])
			i++
			continue
		}

		newInner, ok := processBar(ctx, lines[i+1:j], s)
		if ok {
			changed++
		}
		out = append(out, lines[i])    // открывающее ограждение
		out = append(out, newInner...) // содержимое блока
		out = append(out, lines[j])    // закрывающее ограждение
		i = j + 1
	}

	return strings.Join(out, "\n"), changed
}

// barFields — распознанные поля блока прогресс-бара.
type barFields struct {
	kind, name, goalRaw string
	hasGoal, hasValue   bool
}

// scanBar собирает интересующие поля из строк блока.
func scanBar(inner []string) barFields {
	var b barFields
	for _, l := range inner {
		k, v, ok := splitField(l)
		if !ok {
			continue
		}
		switch k {
		case "kind":
			b.kind = v
		case "name":
			b.name = v
		case "goal":
			b.goalRaw, b.hasGoal = v, true
		case "value":
			b.hasValue = true
		}
	}
	return b
}

// processBar обрабатывает содержимое одного блока (без ограждений). Возвращает
// новые строки и признак изменения. Неизвестные и не-manual блоки не трогает.
func processBar(ctx context.Context, inner []string, s *Snapshot) ([]string, bool) {
	b := scanBar(inner)
	if strings.ToLower(strings.TrimSpace(b.kind)) != "manual" {
		return inner, false
	}
	src, known := barSource(b.name, s)
	if !known {
		return inner, false
	}
	goalK, ok := resolveGoal(ctx, b.name, b.goalRaw, b.hasGoal)
	if !ok {
		return inner, false
	}

	goalRub := tinvest.DecUnits(goalK * goalScale)
	out := rebuildBar(inner, b.hasValue, src.Percent(goalRub).String(0))
	return out, !equalLines(inner, out)
}

// rebuildBar переписывает строки блока: max → 100, value → процент. Прочие строки
// (kind/name/goal/width) сохраняет как есть — goal пишет человек.
func rebuildBar(inner []string, hasValue bool, pctStr string) []string {
	out := make([]string, 0, len(inner)+1)
	for _, l := range inner {
		k, _, ok := splitField(l)
		if !ok {
			out = append(out, l)
			continue
		}
		indent := leadingWS(l)
		switch k {
		case "max":
			out = append(out, indent+"max: "+pbBarMax)
		case "value":
			out = append(out, indent+"value: "+pctStr)
		default:
			out = append(out, l)
		}
	}
	// На случай блока без строки value — добавим её.
	if !hasValue {
		out = append(out, "value: "+pctStr)
	}
	return out
}

// resolveGoal читает цель бара в тысячах рублей из поля goal. Поле обязательно:
// без него (или с нечисловым значением) бар пропускается — задача goal не создаёт.
func resolveGoal(ctx context.Context, name, goalRaw string, hasGoal bool) (int64, bool) {
	if !hasGoal {
		slog.WarnContext(ctx, "progressbar has no goal field, skipping", slog.String("name", name))
		return 0, false
	}
	g, err := strconv.ParseInt(strings.TrimSpace(goalRaw), 10, 64)
	if err != nil {
		slog.WarnContext(ctx, "progressbar goal is not an integer, skipping",
			slog.String("name", name), slog.String("goal", goalRaw))
		return 0, false
	}
	return g, true
}

// splitField разбирает строку «key: value». ok=false для строк без двоеточия.
func splitField(line string) (key, value string, ok bool) {
	i := strings.IndexByte(line, ':')
	if i < 0 {
		return "", "", false
	}
	return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:]), true
}

// leadingWS возвращает ведущие пробелы/табы строки — чтобы отступ не терялся.
func leadingWS(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}

func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
