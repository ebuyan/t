package portfolio

import (
	"strings"

	"tportfolio/internal/tinvest"
)

// Форматирование значений в строки в том виде, в каком они лежат в файлах и на
// странице. Собрано в одном месте: и md-таблицы, и веб-страница печатают одинаково.

// pct форматирует долю так же, как в файле: запятая и знак процента.
func pct(d tinvest.Dec) string {
	return strings.ReplaceAll(d.String(2), ".", ",") + "%"
}

// rub форматирует целые рубли с разделителями тысяч.
func rub(d tinvest.Dec) string {
	return tinvest.Group(d.String(0))
}

// money — целые рубли с разделителями тысяч и знаком валюты.
func money(d tinvest.Dec) string { return rub(d) + " ₽" }

// price — цена за штуку: два знака после запятой (у цен бывают копейки).
func price(d tinvest.Dec) string {
	return strings.ReplaceAll(tinvest.Group(d.String(2)), ".", ",") + " ₽"
}

// signedMoney добавляет «+» к положительной сумме (у отрицательной знак уже есть).
func signedMoney(d tinvest.Dec) string {
	if d.Sign() > 0 {
		return "+" + money(d)
	}
	return money(d)
}

// signedPct — то же для процента доходности.
func signedPct(d tinvest.Dec) string {
	if d.Sign() > 0 {
		return "+" + pct(d)
	}
	return pct(d)
}
