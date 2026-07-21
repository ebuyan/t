package portfolio

import (
	"sort"
	"time"

	"tportfolio/internal/tinvest"
)

// YieldView — модель веб-страницы доходности: всё уже отформатировано в строки,
// чтобы шаблон в пакете web ничего не считал.
type YieldView struct {
	Updated      string
	Total        string // стоимость акции + золото
	Income       string // абсолютный доход за всё время (акции + золото)
	IncomePct    string // относительная доходность
	IncomePos    bool
	DayChange    string // изменение за сегодня, рубли
	DayChangePct string // изменение за сегодня, проценты
	DayChangePos bool
	Shares       AssetView
	Gold         AssetView
	Holdings     []HoldingView
	// CanSync — показывать ли кнопку записи текущих значений в реестр. Ставится
	// сервером: доступно, только если реестр сконфигурирован.
	CanSync bool
}

type AssetView struct {
	Name     string
	Value    string
	Share    string // доля от базы (акции + золото)
	Yield    string // абсолютный доход
	YieldPct string // относительная доходность
	Positive bool
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
}

// BuildYieldView строит модель страницы из среза. Названия бумаг берутся из meta
// (может быть nil — тогда показываем только тикеры): на странице это не критично.
func BuildYieldView(s *Snapshot, m *Meta, updated time.Time) YieldView {
	income := s.StockYield.Add(s.GoldYield)

	v := YieldView{
		Updated:      updated.Format("2006-01-02 15:04:05 MST"),
		Total:        money(s.Total),
		Income:       signedMoney(income),
		IncomePct:    signedPct(income.Percent(s.Total.Sub(income))),
		IncomePos:    income.Sign() >= 0,
		DayChange:    signedMoney(s.DayChange),
		DayChangePct: signedPct(s.DayChangePct),
		DayChangePos: s.DayChange.Sign() >= 0,
		Shares:       buildAsset("Акции", s.Shares, s.StockYield, s.Total),
		Gold:         buildAsset("Золото", s.Gold, s.GoldYield, s.Total),
	}
	v.Holdings = buildHoldings(s, m)
	return v
}

// holdingRow — строка состава: стоимость для сортировки и уже готовое представление.
type holdingRow struct {
	value tinvest.Dec
	view  HoldingView
}

// buildHoldings собирает таблицу состава: бумаги, золото и кеш одной таблицей,
// отсортированные по стоимости.
func buildHoldings(s *Snapshot, m *Meta) []HoldingView {
	rows := make([]holdingRow, 0, len(s.Holdings)+2)

	for _, h := range s.Holdings {
		name := ""
		if m != nil {
			name = m.Names[h.UID]
		}
		rows = append(rows, holdingRow{h.Value, HoldingView{
			Ticker:         h.Ticker,
			Name:           name,
			Price:          price(h.Price),
			Value:          money(h.Value),
			Share:          pct(h.Value.Percent(s.Total)),
			Yield:          signedMoney(h.Yield),
			YieldClass:     signClass(h.Yield),
			DayChange:      signedMoney(h.DayChange),
			DayChangeClass: signClass(h.DayChange),
		}})
	}

	if !s.Gold.IsZero() {
		rows = append(rows, holdingRow{s.Gold, HoldingView{
			Ticker:         "GLDRUB_TOM",
			Name:           "Золото",
			Price:          dash,
			Value:          money(s.Gold),
			Share:          pct(s.Gold.Percent(s.Total)),
			Yield:          signedMoney(s.GoldYield),
			YieldClass:     signClass(s.GoldYield),
			DayChange:      signedMoney(s.GoldDayChange),
			DayChangeClass: signClass(s.GoldDayChange),
		}})
	}

	if !s.Cash.IsZero() {
		// Кеш: доли/доходности нет, показываем только сумму — по ней видно приход
		// дивидендов.
		rows = append(rows, holdingRow{s.Cash, HoldingView{
			Ticker:    "RUB",
			Name:      "Кеш",
			Price:     dash,
			Value:     money(s.Cash),
			Share:     dash,
			Yield:     dash,
			DayChange: dash,
		}})
	}

	sort.Slice(rows, func(i, j int) bool {
		return rows[i].value.Cmp(rows[j].value) > 0
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
