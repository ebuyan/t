package portfolio

import (
	"sort"
	"time"

	"tinvest/internal/tinvest"
)

// YieldView — модель веб-страницы доходности: всё уже отформатировано в строки,
// чтобы шаблон в пакете web ничего не считал.
type YieldView struct {
	Updated      string
	Total        string // полная стоимость портфеля (акции + золото + кеш)
	Income       string // доход за всё время: переоценка позиций плюс дивиденды
	IncomePct    string // относительная доходность
	IncomePos    bool
	DayChange    string // изменение за сегодня, рубли
	DayChangePct string // изменение за сегодня, проценты
	DayChangePos bool
	Shares       AssetView
	Gold         AssetView
	Realty       AssetView
	Holdings     []HoldingView
	// CanSync — показывать ли кнопку записи текущих значений в реестр. Ставится
	// сервером: доступно, только если реестр сконфигурирован.
	CanSync bool
	// CanSyncPortfolio — показывать ли кнопку фиксации долей в Портфель.md.
	// Ставится сервером: доступно, только если файл долей сконфигурирован.
	CanSyncPortfolio bool
}

type AssetView struct {
	Name     string
	Value    string
	Share    string // доля от базы (акции + золото + недвижимость + кеш)
	Yield    string // абсолютный доход
	YieldPct string // относительная доходность
	Positive bool
	// PayoutLabel/Payout — строка полученных выплат в карточке класса
	// («Дивиденды» у акций, «Рента» у недвижимости); пустая подпись — строки нет.
	PayoutLabel string
	Payout      string
	PayoutPos   bool
}

type HoldingView struct {
	Ticker         string
	Name           string
	Price          string // текущая цена за штуку («—» для золота и кеша)
	Value          string
	Share          string
	Yield          string // за всё время
	YieldClass     string // pos | neg | "" (нейтрально, для «—»)
	DayChange      string // за сегодня
	DayChangeClass string
	// Ключи сортировки столбцов на странице — числа строкой (точка, без
	// разделителей); пустой ключ — значения нет, такая строка уходит в конец.
	ShareKey     string
	YieldKey     string
	DayChangeKey string
}

// BuildYieldView строит модель страницы из среза. Названия бумаг берутся из meta
// (может быть nil — тогда показываем только тикеры): на странице это не критично.
// payouts — полученные за всё время выплаты; в доходность позиций они не входят,
// поэтому прибавляются к доходу отдельно. В карточках классов выплаты разнесены:
// рента (по паям недвижимости) — у недвижимости, остальное — дивиденды у акций.
func BuildYieldView(s *Snapshot, m *Meta, payouts Payouts, updated time.Time) YieldView {
	yield := s.Yield() // переоценка позиций, без выплат
	income := yield.Add(payouts.Total)
	base := s.ShareBase() // доли считаем от акции + золото + недвижимость + кеш

	v := YieldView{
		Updated: updated.Format("2006-01-02 15:04:05 MST"),
		Total:   money(s.PortfolioValue),
		Income:  signedMoney(income),
		// Доходность — к вложенному в акции + золото + недвижимость (кеш дохода
		// не даёт).
		// Дивиденды уже выведены из позиций, поэтому знаменатель считаем от
		// переоценки, а не от полного дохода.
		IncomePct:    signedPct(income.Percent(s.Total.Sub(yield))),
		IncomePos:    income.Sign() >= 0,
		DayChange:    signedMoney(s.DayChange),
		DayChangePct: signedPct(s.DayChangePct),
		DayChangePos: s.DayChange.Sign() >= 0,
		Shares:       buildAsset("Акции", s.Shares, s.StockYield, base),
		Gold:         buildAsset("Золото", s.Gold, s.GoldYield, base),
		Realty:       buildAsset("Недвижимость", s.Realty, s.RealtyYield, base),
		Holdings:     buildHoldings(s, m),
	}
	v.Shares.withPayout("Дивиденды", payouts.Total.Sub(payouts.Rent))
	v.Realty.withPayout("Рента", payouts.Rent)
	return v
}

// holdingRow — строка состава: изменение за сегодня для сортировки и уже готовое
// представление.
type holdingRow struct {
	dayChange tinvest.Dec
	view      HoldingView
}

// buildHoldings собирает таблицу состава: бумаги, фонды недвижимости, золото и
// кеш одной таблицей, отсортированные по убыванию изменения за сегодня.
func buildHoldings(s *Snapshot, m *Meta) []HoldingView {
	rows := make([]holdingRow, 0, len(s.Holdings)+len(s.RealtyHoldings)+2)
	base := s.ShareBase() // доли строк — от акции + золото + недвижимость + кеш

	for _, h := range append(append([]Holding{}, s.Holdings...), s.RealtyHoldings...) {
		ticker, name := HoldingLabel(&h, m)
		rows = append(rows, holdingRow{h.DayChange, HoldingView{
			Ticker:         ticker,
			Name:           name,
			Price:          price(h.Price),
			Value:          money(h.Value),
			Share:          pct(h.Value.Percent(base)),
			Yield:          signedMoney(h.Yield),
			YieldClass:     signClass(h.Yield),
			DayChange:      signedMoney(h.DayChange),
			DayChangeClass: signClass(h.DayChange),
			ShareKey:       sortKey(h.Value.Percent(base)),
			YieldKey:       sortKey(h.Yield),
			DayChangeKey:   sortKey(h.DayChange),
		}})
	}

	if !s.Gold.IsZero() {
		rows = append(rows, holdingRow{s.GoldDayChange, HoldingView{
			Ticker:         "GLDRUB_TOM",
			Name:           "Золото",
			Price:          dash,
			Value:          money(s.Gold),
			Share:          pct(s.Gold.Percent(base)),
			Yield:          signedMoney(s.GoldYield),
			YieldClass:     signClass(s.GoldYield),
			DayChange:      signedMoney(s.GoldDayChange),
			DayChangeClass: signClass(s.GoldDayChange),
			ShareKey:       sortKey(s.Gold.Percent(base)),
			YieldKey:       sortKey(s.GoldYield),
			DayChangeKey:   sortKey(s.GoldDayChange),
		}})
	}

	if !s.Cash.IsZero() {
		// Кеш: доля от той же базы (входит в 100%); доходности нет — по сумме
		// видно приход дивидендов.
		// Изменения за сегодня у кеша нет — показываем нулём (зелёным) и
		// сортируем как нулевой.
		var zero tinvest.Dec
		rows = append(rows, holdingRow{zero, HoldingView{
			Ticker:         "RUB",
			Name:           "Кеш",
			Price:          dash,
			Value:          money(s.Cash),
			Share:          pct(s.Cash.Percent(base)),
			Yield:          dash,
			DayChange:      signedMoney(zero),
			DayChangeClass: signClass(zero),
			ShareKey:       sortKey(s.Cash.Percent(base)),
			DayChangeKey:   sortKey(zero),
		}})
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].dayChange.Cmp(rows[j].dayChange) > 0
	})

	views := make([]HoldingView, 0, len(rows))
	for i := range rows {
		views = append(views, rows[i].view)
	}
	return views
}

// dash — прочерк в ячейках, где значения нет (цена/доля/доходность у кеша и т.п.).
const dash = "—"

// signClass возвращает CSS-класс подсветки по знаку: pos для неотрицательного,
// neg для отрицательного.
func signClass(d tinvest.Dec) string {
	if d.Sign() < 0 {
		return "neg"
	}
	return "pos"
}

// sortKey — ключ сортировки столбца: число с точкой, без разделителей тысяч.
func sortKey(d tinvest.Dec) string { return d.String(2) }

// withPayout добавляет в карточку класса строку полученных выплат.
func (a *AssetView) withPayout(label string, d tinvest.Dec) {
	a.PayoutLabel = label
	a.Payout = signedMoney(d)
	a.PayoutPos = d.Sign() >= 0
}

// buildAsset собирает карточку класса активов. Доходность считается к вложенному
// (стоимость − доход), как в приложении Т-Банка.
func buildAsset(name string, value, yield, total tinvest.Dec) AssetView {
	return AssetView{
		Name:     name,
		Value:    money(value),
		Share:    pct(value.Percent(total)),
		Yield:    signedMoney(yield),
		YieldPct: signedPct(yield.Percent(value.Sub(yield))),
		Positive: yield.Sign() >= 0,
	}
}
