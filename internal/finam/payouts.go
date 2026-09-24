package finam

import "tinvest/internal/tinvest"

// Payouts — итог по выплатам счёта: дивиденды, купоны и выплаты по паям фондов.
type Payouts struct {
	// Net — выплаты за вычетом налога, удержанного по тем же бумагам.
	Net tinvest.Dec
	// Skipped — транзакции, которые не удалось учесть (не в рублях или
	// неразборчивая сумма): их список уходит в лог, чтобы сумма не была молча
	// заниженной.
	Skipped []string
}

// SumPayouts суммирует выплаты по транзакциям счёта. В сумму идут доходы
// (INCOME) с привязкой к бумаге и налоги (TAX) по бумагам, по которым был доход:
// у налога сумма уже отрицательная. Доход без бумаги (например, процент на
// остаток) и налог по бумагам без выплат (налог с продажи) не считаются — это не
// выплаты по позициям.
func SumPayouts(txs []Transaction) Payouts {
	paid := map[string]bool{}
	for i := range txs {
		if txs[i].Category == CategoryIncome && txs[i].Symbol != "" {
			paid[txs[i].Symbol] = true
		}
	}

	var p Payouts
	for i := range txs {
		tx := &txs[i]
		switch {
		case tx.Category == CategoryIncome && tx.Symbol != "":
		case tx.Category == CategoryTax && paid[tx.Symbol]:
		default:
			continue
		}
		if !tx.Change.IsRUB() {
			p.Skipped = append(p.Skipped, tx.Symbol)
			continue
		}
		v, err := tx.Change.Dec()
		if err != nil {
			p.Skipped = append(p.Skipped, tx.Symbol)
			continue
		}
		p.Net = p.Net.Add(v)
	}
	return p
}
