package web_test

import (
	"strings"
	"testing"
	"time"

	"tinvest/internal/portfolio"
	"tinvest/internal/tinvest"
	"tinvest/web"
)

func TestPageRenders(t *testing.T) {
	s := &portfolio.Snapshot{
		Date:       time.Date(2026, 7, 21, 10, 0, 0, 0, time.UTC),
		Shares:     tinvest.DecUnits(1_000_000),
		Gold:       tinvest.DecUnits(500_000),
		StockYield: tinvest.DecUnits(100_000),
		GoldYield:  tinvest.DecUnits(-50_000),
		Total:      tinvest.DecUnits(1_500_000),
		Holdings:   []portfolio.Holding{{Ticker: "SBER", Value: tinvest.DecUnits(600_000), UID: "uid-sber"}},
	}
	m := &portfolio.Meta{Names: map[string]string{"uid-sber": "Сбер Банк"}}

	view := portfolio.BuildYieldView(s, m, s.Date)
	view.CanSync = true

	var b strings.Builder
	if err := web.Page.Execute(&b, view); err != nil {
		t.Fatalf("рендер страницы: %v", err)
	}
	out := b.String()
	// html/template экранирует «+» как &#43; (в браузере — обычный «+»),
	// поэтому сумму дохода ищем без ведущего знака.
	for _, want := range []string{
		"Портфель", "SBER", "Сбер Банк", "50 000 ₽", "66,67%",
		"Изменение за сегодня", "Цена", "За всё время", "За сегодня", "Золото", "Записать в реестр",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("в выводе нет %q", want)
		}
	}
}

func TestErrorRenders(t *testing.T) {
	var b strings.Builder
	if err := web.Error.Execute(&b, "срез ещё не собран"); err != nil {
		t.Fatalf("рендер заглушки: %v", err)
	}
	if !strings.Contains(b.String(), "срез ещё не собран") {
		t.Error("в заглушке нет сообщения об ошибке")
	}
}
