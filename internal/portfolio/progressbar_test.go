package portfolio

import (
	"strings"
	"testing"

	"tinvest/internal/tinvest"
)

// testDoc — документ с автоматическим баром day-year и четырьмя manual-барами в
// целевом формате (goal задан, max: 100), но с устаревшим value, плюс текст вне
// блоков.
const testDoc = "" +
	"```progressbar\n" +
	"kind: day-year\n" +
	"name: Year\n" +
	"width: 100%\n" +
	"```\n" +
	"```progressbar\n" +
	"kind: manual\n" +
	"name: Total\n" +
	"goal: 10000\n" +
	"max: 100\n" +
	"value: 0\n" +
	"width: 100%\n" +
	"```\n" +
	"```progressbar\n" +
	"kind: manual\n" +
	"name: Gold\n" +
	"goal: 1500\n" +
	"max: 100\n" +
	"value: 0\n" +
	"width: 100%\n" +
	"```\n" +
	"```progressbar\n" +
	"kind: manual\n" +
	"name: Stoc\n" +
	"goal: 7500\n" +
	"max: 100\n" +
	"value: 0\n" +
	"width: 100%\n" +
	"```\n" +
	"```progressbar\n" +
	"kind: manual\n" +
	"name: Reits\n" +
	"goal: 1000\n" +
	"max: 100\n" +
	"value: 99\n" +
	"width: 100%\n" +
	"```\n" +
	"```progressbar\n" +
	"kind: manual\n" +
	"name: Cash\n" +
	"goal: 500\n" +
	"max: 100\n" +
	"value: 0\n" +
	"width: 100%\n" +
	"```\n" +
	"\nТекст под барами не трогаем.\n"

// snap — 4.7М всего, 3.75М акции, 0.75М золото, 0.2М кэш (рубли + LQDT).
func testSnap() *Snapshot {
	return &Snapshot{
		PortfolioValue: tinvest.DecUnits(4_700_000),
		Shares:         tinvest.DecUnits(3_750_000),
		Gold:           tinvest.DecUnits(750_000),
		Cash:           tinvest.DecUnits(200_000),
	}
}

func TestApplyProgressBars(t *testing.T) {
	out, n := applyProgressBars(t.Context(), testDoc, testSnap())

	if n != 5 {
		t.Errorf("изменено баров = %d, хотим 5", n)
	}

	// value = накоплено ÷ (goal×1000) × 100. goal человек не трогаем.
	// Total: 4.7М/10М=47, Gold: 0.75М/1.5М=50, Stoc: 3.75М/7.5М=50, Reits: 0,
	// Cash: 0.2М/0.5М=40.
	for _, want := range []string{
		"name: Total\ngoal: 10000\nmax: 100\nvalue: 47\n",
		"name: Gold\ngoal: 1500\nmax: 100\nvalue: 50\n",
		"name: Stoc\ngoal: 7500\nmax: 100\nvalue: 50\n",
		"name: Reits\ngoal: 1000\nmax: 100\nvalue: 0\n",
		"name: Cash\ngoal: 500\nmax: 100\nvalue: 40\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("в выводе нет блока:\n%s", want)
		}
	}

	// Бар day-year не трогаем.
	if !strings.Contains(out, "kind: day-year\nname: Year\nwidth: 100%") {
		t.Error("бар day-year изменён, а не должен")
	}
	// Текст вне блоков сохранён.
	if !strings.Contains(out, "Текст под барами не трогаем.") {
		t.Error("текст вне блоков потерян")
	}
}

func TestApplyProgressBarsIdempotent(t *testing.T) {
	once, _ := applyProgressBars(t.Context(), testDoc, testSnap())
	twice, n := applyProgressBars(t.Context(), once, testSnap())

	if n != 0 {
		t.Errorf("повторный прогон изменил %d баров, хотим 0", n)
	}
	if once != twice {
		t.Error("повторный прогон изменил уже актуальный файл")
	}
}

func TestApplyProgressBarsSkipsWithoutGoal(t *testing.T) {
	// Без поля goal бар не трогаем: миграции из max больше нет.
	doc := "```progressbar\n" +
		"kind: manual\n" +
		"name: Total\n" +
		"max: 10000\n" +
		"value: 4700\n" +
		"```\n"
	out, n := applyProgressBars(t.Context(), doc, testSnap())

	if n != 0 {
		t.Errorf("изменено баров = %d, хотим 0 (нет goal)", n)
	}
	if out != doc {
		t.Errorf("бар без goal изменён, а не должен:\n%s", out)
	}
}
