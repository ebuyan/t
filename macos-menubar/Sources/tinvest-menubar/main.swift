import AppKit

// Меню-бар со сводкой «всего за сегодня» из tinvest.
// Адрес сервиса берётся из переменной окружения TINVEST_URL, иначе — из
// значения по умолчанию ниже. Замени IP на свой хост со стеком ha.
let defaultURL = "http://192.168.0.108:8077/api/today"

// Как часто опрашивать сервис. Данные на сервере обновляются раз в минуту.
let refreshInterval: TimeInterval = 60

// Модель ответа /api/today. Все суммы в рублях, dayChangePct — в процентах.
// Проценты долей и доходности считаем на месте (см. render).
struct Asset: Decodable {
    let value: Double
    let yield: Double
}

struct Holding: Decodable {
    // assetClass — класс строки: shares | gold | realty. Опционально: старая
    // сборка сервиса поля не отдаёт, тогда класс угадываем по тикеру (inferredClass).
    let assetClass: String?
    let ticker: String
    let name: String?
    let value: Double
    let dayChange: Double

    enum CodingKeys: String, CodingKey {
        case ticker, name, value
        case assetClass = "class"
        case dayChange = "day_change"
    }

    // inferredClass — класс строки; для ответа без поля class золото узнаём по
    // тикеру GLDRUB_*, остальное — акции (недвижимости старая сборка не знает).
    var inferredClass: String {
        if let c = assetClass { return c }
        return ticker.hasPrefix("GLDRUB") ? "gold" : "shares"
    }
}

struct Today: Decodable {
    let portfolioValue: Double
    let total: Double
    let dayChange: Double
    let dayChangePct: Double
    // income — курсовая переоценка позиций (акции + золото), без дивидендов.
    let income: Double
    // dividends — полученные за всё время выплаты за вычетом налога, включая
    // ренту. Опционально: старая сборка сервиса поля ещё не отдаёт.
    let dividends: Double?
    // rent — часть dividends: рента по паям фондов недвижимости. Опционально:
    // без поля вся сумма показывается дивидендами.
    let rent: Double?
    let shares: Asset
    let gold: Asset
    // realty — недвижимость (паи ЗПИФ на Финаме). Опционально: старая сборка
    // сервиса поля не отдаёт — тогда пункта нет.
    let realty: Asset?
    let cash: Double
    let holdings: [Holding]
    let updated: String

    enum CodingKeys: String, CodingKey {
        case portfolioValue = "portfolio_value"
        case total, income, dividends, rent, shares, gold, realty, cash, holdings, updated
        case dayChange = "day_change"
        case dayChangePct = "day_change_pct"
    }
}

final class AppDelegate: NSObject, NSApplicationDelegate {
    private var statusItem: NSStatusItem!
    private var timer: Timer?
    private let endpoint: URL

    // latest — последний успешный ответ, чтобы перерисовывать при переключении
    // режима скрытия без нового запроса. hidden — скрыты ли цифры (для шаринга экрана).
    private var latest: Today?
    private var hidden = false

    override init() {
        let raw = ProcessInfo.processInfo.environment["TINVEST_URL"] ?? defaultURL
        guard let url = URL(string: raw) else {
            fatalError("invalid TINVEST_URL: \(raw)")
        }
        endpoint = url
        super.init()
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        statusItem.button?.title = "…"
        statusItem.menu = placeholderMenu("Загрузка…")

        fetch()
        timer = Timer.scheduledTimer(withTimeInterval: refreshInterval, repeats: true) { [weak self] _ in
            self?.fetch()
        }
    }

    @objc private func refreshNow() { fetch() }

    @objc private func quit() { NSApplication.shared.terminate(nil) }

    @objc private func toggleHidden() {
        hidden.toggle()
        renderCurrent()
    }

    // renderCurrent перерисовывает по текущему состоянию: маскирует при hidden.
    private func renderCurrent() {
        if hidden {
            renderHidden()
            return
        }
        guard let t = latest else { return }
        render(t)
    }

    // renderHidden прячет все цифры — и в строке меню, и в выпадашке.
    private func renderHidden() {
        statusItem.button?.attributedTitle = NSAttributedString(
            string: "•••",
            attributes: [.foregroundColor: NSColor.secondaryLabelColor]
        )
        let menu = NSMenu()
        menu.addItem(NSMenuItem(title: "Содержимое скрыто", action: nil, keyEquivalent: ""))
        menu.addItem(.separator())
        menu.addItem(hideToggleItem())
        menu.addItem(withKey("Обновить сейчас", #selector(refreshNow), "r"))
        menu.addItem(withKey("Выход", #selector(quit), "q"))
        statusItem.menu = menu
    }

    // fetch дёргает /api/today и перерисовывает строку меню и выпадашку.
    private func fetch() {
        var req = URLRequest(url: endpoint)
        req.timeoutInterval = 10
        URLSession.shared.dataTask(with: req) { [weak self] data, resp, err in
            guard let self else { return }
            if let err {
                DispatchQueue.main.async { self.showError("сеть: \(err.localizedDescription)") }
                return
            }
            guard let http = resp as? HTTPURLResponse, http.statusCode == 200, let data else {
                let code = (resp as? HTTPURLResponse)?.statusCode ?? 0
                DispatchQueue.main.async { self.showError("HTTP \(code)") }
                return
            }
            do {
                let today = try JSONDecoder().decode(Today.self, from: data)
                DispatchQueue.main.async {
                    self.latest = today
                    self.renderCurrent()
                }
            } catch {
                DispatchQueue.main.async { self.showError("разбор ответа") }
            }
        }.resume()
    }

    // render печатает сводку. В строке меню — изменение за день (цветом),
    // подробности — в выпадашке.
    private func render(_ t: Today) {
        let up = t.dayChange >= 0
        let arrow = up ? "▲" : "▼"
        statusItem.button?.attributedTitle = NSAttributedString(
            string: "\(arrow) \(signedRub(t.dayChange))",
            attributes: [.foregroundColor: up ? NSColor.systemGreen : NSColor.systemRed]
        )

        // Доход за всё время = курсовая переоценка + полученные выплаты.
        // Знаменатель — вложенное в акции, золото и недвижимость (стоимость минус
        // курсовой доход): выплаты уже выведены из позиций и лежат в кеше.
        let dividends = t.dividends ?? 0
        let income = t.income + dividends
        let invested = t.total - t.income
        // База долей — акции + золото + недвижимость + кеш (в сумме 100%).
        // t.total (без кеша) оставляем для доходности выше — кеш дохода не даёт.
        let realtyValue = t.realty?.value ?? 0
        let shareBase = t.shares.value + t.gold.value + realtyValue + t.cash
        // Классы раскрываются подменю со своими бумагами — отдельного «Состава» нет.
        let byClass = Dictionary(grouping: t.holdings, by: { $0.inferredClass })

        // nil в списке — разделитель. Все строки сводки — одна таблица: колонки
        // значений выровнены через общие табуляции.
        var rows: [Row?] = [
            Row(label: "Стоимость портфеля", cols: [rub(t.portfolioValue)]),
            Row(label: "За сегодня", cols: [signedRub(t.dayChange), signedPct(t.dayChangePct)], sign: t.dayChange),
            Row(label: "За всё время", cols: [signedRub(income), signedPct(pctOf(income, invested))], sign: income),
            nil,
            assetRow("Акции", t.shares, base: shareBase, holdings: byClass["shares"] ?? []),
            assetRow("Золото", t.gold, base: shareBase, holdings: byClass["gold"] ?? []),
        ]
        if let realty = t.realty {
            rows.append(assetRow("Недвижимость", realty, base: shareBase, holdings: byClass["realty"] ?? []))
        }
        if t.cash != 0 {
            // Кеш: доля от той же базы, доходности у кеша нет.
            rows.append(Row(label: "Кеш", cols: [rub(t.cash), pct(pctOf(t.cash, shareBase))], sign: t.cash))
        }
        // Дивиденды и рента — не классы активов, а суммы выплат за всё время,
        // поэтому без доли: деньги уже лежат в кеше или вложены обратно в бумаги.
        // Рента (выплаты по паям недвижимости) — отдельной строкой под дивидендами,
        // всегда, даже нулём, если сервис отдаёт поле: старая сборка его не знает.
        let rent = t.rent ?? 0
        let stockDividends = dividends - rent
        if stockDividends != 0 {
            rows.append(Row(label: "Дивиденды", cols: [rub(stockDividends)], sign: stockDividends))
        }
        if t.rent != nil {
            rows.append(Row(label: "Рента", cols: [rub(rent)], sign: rent))
        }
        rows.append(nil)

        let menu = NSMenu()
        for item in tableItems(rows) {
            menu.addItem(item)
        }
        menu.addItem(hideToggleItem())
        menu.addItem(withKey("Обновить сейчас", #selector(refreshNow), "r"))
        menu.addItem(withKey("Выход", #selector(quit), "q"))
        menu.addItem(.separator())
        menu.addItem(footerItem("Обновлено в \(shortTime(t.updated))"))
        statusItem.menu = menu
    }

    // footerItem — подвал меню: неактивная подпись по центру. Обычный пункт текст
    // не центрирует, поэтому это пункт со своим view: меню растягивает его на всю
    // ширину (autoresizingMask), а надпись выравнивается по центру внутри.
    private func footerItem(_ text: String) -> NSMenuItem {
        let label = NSTextField(labelWithString: text)
        label.font = NSFont.menuFont(ofSize: NSFont.smallSystemFontSize)
        label.textColor = .disabledControlTextColor
        label.alignment = .center
        label.sizeToFit()

        let height = label.frame.height + 6
        let view = NSView(frame: NSRect(x: 0, y: 0, width: label.frame.width + 40, height: height))
        view.autoresizingMask = [.width]
        label.frame = NSRect(x: 0, y: 3, width: view.frame.width, height: label.frame.height)
        label.autoresizingMask = [.width]
        view.addSubview(label)

        let item = NSMenuItem()
        item.view = view
        item.isEnabled = false
        return item
    }

    // hideToggleItem — переключатель скрытия содержимого (для шаринга экрана).
    private func hideToggleItem() -> NSMenuItem {
        let title = hidden ? "Показать содержимое" : "Скрыть содержимое"
        let item = NSMenuItem(title: title, action: #selector(toggleHidden), keyEquivalent: "h")
        item.state = hidden ? .on : .off
        return item
    }

    private func showError(_ msg: String) {
        // В скрытом режиме не раскрываем меню: ошибки не содержат цифр, но пусть
        // содержимое остаётся замаскированным до явного «Показать».
        if hidden { return }
        statusItem.button?.attributedTitle = NSAttributedString(
            string: "⚠︎",
            attributes: [.foregroundColor: NSColor.systemOrange]
        )
        let menu = placeholderMenu("Ошибка: \(msg)")
        menu.addItem(withKey("Обновить сейчас", #selector(refreshNow), "r"))
        menu.addItem(withKey("Выход", #selector(quit), "q"))
        statusItem.menu = menu
    }

    // --- Сборка пунктов меню ---

    // assetRow — строка класса активов: стоимость, доля от базы и доходность.
    // Если у класса есть бумаги, пункт раскрывается подменю с ними.
    private func assetRow(_ name: String, _ a: Asset, base: Double, holdings: [Holding]) -> Row {
        Row(
            label: name,
            cols: [rub(a.value), pct(pctOf(a.value, base)), signedPct(yieldPct(a.value, a.yield))],
            sign: a.yield,
            submenu: holdings.isEmpty ? nil : holdingsSubmenu(holdings)
        )
    }

    // holdingsSubmenu — бумаги класса с изменением за сегодня, по убыванию
    // изменения (сверху — сильнее всего выросшие за день).
    private func holdingsSubmenu(_ holdings: [Holding]) -> NSMenu {
        let sub = NSMenu()
        let rows: [Row?] = holdings.sorted(by: { $0.dayChange > $1.dayChange }).map { h in
            Row(
                label: holdingLabel(h),
                cols: [rub(h.value), signedRub(h.dayChange)],
                sign: h.dayChange
            )
        }
        for item in tableItems(rows) {
            sub.addItem(item)
        }
        return sub
    }

    // holdingLabel — подпись бумаги в подменю: «тикер — название». У фонда
    // недвижимости код пая обычно равен ISIN и ничего не говорит, поэтому
    // показываем только название (короткое имя фонда от сервиса).
    private func holdingLabel(_ h: Holding) -> String {
        guard let name = h.name, !name.isEmpty else { return h.ticker }
        return h.inferredClass == "realty" ? name : "\(h.ticker) — \(name)"
    }

    // tableItems собирает пункты меню из строк: подпись слева, значения — по
    // колонкам с правым выравниванием на общих табуляциях (цифры моноширинные,
    // поэтому разряды встают друг под другом). nil — разделитель.
    //
    // У строк есть пустое действие: без него AppKit считает пункт неактивным и
    // приглушает его, а пункт с подменю — активным. С действием все строки
    // активные и выглядят одинаково.
    private func tableItems(_ rows: [Row?]) -> [NSMenuItem] {
        let present = rows.compactMap { $0 }
        let labelWidth = present.map { textWidth($0.label, menuFont) }.max() ?? 0
        let columns = present.map { $0.cols.count }.max() ?? 0
        var colWidth = [CGFloat](repeating: 0, count: columns)
        for r in present {
            for (i, c) in r.cols.enumerated() {
                colWidth[i] = max(colWidth[i], textWidth(c, valueFont))
            }
        }

        var tabs: [NSTextTab] = []
        var x = labelWidth + labelGap
        for w in colWidth {
            x += w
            tabs.append(NSTextTab(textAlignment: .right, location: x, options: [:]))
            x += columnGap
        }
        let para = NSMutableParagraphStyle()
        para.tabStops = tabs

        return rows.map { row in
            guard let r = row else { return .separator() }
            let item = NSMenuItem(title: ([r.label] + r.cols).joined(separator: " "), action: #selector(noop), keyEquivalent: "")
            item.target = self
            let s = NSMutableAttributedString(
                string: r.label,
                attributes: [.font: menuFont, .foregroundColor: labelColor, .paragraphStyle: para]
            )
            let valueColor = r.sign.map(signColor) ?? labelColor
            for c in r.cols {
                s.append(NSAttributedString(
                    string: "\t" + c,
                    attributes: [.font: valueFont, .foregroundColor: valueColor, .paragraphStyle: para]
                ))
            }
            item.attributedTitle = s
            item.submenu = r.submenu
            return item
        }
    }

    // noop — пустое действие для строк сводки (см. tableItems).
    @objc private func noop() {}

    private func placeholderMenu(_ title: String) -> NSMenu {
        let menu = NSMenu()
        menu.addItem(NSMenuItem(title: title, action: nil, keyEquivalent: ""))
        menu.addItem(.separator())
        return menu
    }

    private func withKey(_ title: String, _ action: Selector, _ key: String) -> NSMenuItem {
        NSMenuItem(title: title, action: action, keyEquivalent: key)
    }
}

// Row — строка сводки: подпись и значения по колонкам. sign окрашивает значения
// по знаку (nil — нейтральные), submenu — раскрывающиеся бумаги класса.
struct Row {
    let label: String
    let cols: [String]
    var sign: Double?
    var submenu: NSMenu?
}

// --- Оформление строк сводки ---

private let menuFont = NSFont.menuFont(ofSize: 0)
// Цифры моноширинные, чтобы разряды в колонках вставали друг под другом.
private let valueFont = NSFont.monospacedDigitSystemFont(ofSize: menuFont.pointSize, weight: .regular)
// Отступ между подписью и первой колонкой и между колонками значений.
private let labelGap: CGFloat = 24
private let columnGap: CGFloat = 14

// Подписи и нейтральные значения — обычным цветом текста меню. Зелёный и красный
// у значений — мягче системных, отдельно для светлой и тёмной темы.
private let labelColor = NSColor.labelColor

private func softColor(light: UInt32, dark: UInt32) -> NSColor {
    NSColor(name: nil) { appearance in
        let isDark = appearance.bestMatch(from: [.aqua, .darkAqua]) == .darkAqua
        let hex = isDark ? dark : light
        return NSColor(
            srgbRed: CGFloat((hex >> 16) & 0xFF) / 255,
            green: CGFloat((hex >> 8) & 0xFF) / 255,
            blue: CGFloat(hex & 0xFF) / 255,
            alpha: 1
        )
    }
}

private let softGreen = softColor(light: 0x3A9D66, dark: 0x6FCF97)
private let softRed = softColor(light: 0xC8585A, dark: 0xEB8585)

private func signColor(_ sign: Double) -> NSColor {
    sign >= 0 ? softGreen : softRed
}

private func textWidth(_ s: String, _ font: NSFont) -> CGFloat {
    ceil((s as NSString).size(withAttributes: [.font: font]).width)
}

// --- Форматирование в русском стиле: пробелы-разделители тысяч, запятая, ₽. ---

private let rubFormatter: NumberFormatter = {
    let f = NumberFormatter()
    f.numberStyle = .decimal
    f.locale = Locale(identifier: "ru_RU")
    f.maximumFractionDigits = 0
    return f
}()

private let pctFormatter: NumberFormatter = {
    let f = NumberFormatter()
    f.numberStyle = .decimal
    f.locale = Locale(identifier: "ru_RU")
    f.minimumFractionDigits = 2
    f.maximumFractionDigits = 2
    return f
}()

private func rub(_ v: Double) -> String {
    let s = rubFormatter.string(from: NSNumber(value: v)) ?? "\(Int(v))"
    return "\(s) ₽"
}

private func signedRub(_ v: Double) -> String {
    (v > 0 ? "+" : "") + rub(v)
}

private func pct(_ v: Double) -> String {
    (pctFormatter.string(from: NSNumber(value: v)) ?? "\(v)") + "%"
}

private func signedPct(_ v: Double) -> String {
    (v > 0 ? "+" : "") + pct(v)
}

// pctOf — доля part от base в процентах.
private func pctOf(_ part: Double, _ base: Double) -> Double {
    base == 0 ? 0 : part / base * 100
}

// yieldPct — относительная доходность: доход к вложенному (стоимость − доход),
// как считает страница и приложение Т-Банка.
private func yieldPct(_ value: Double, _ yield: Double) -> Double {
    let invested = value - yield
    return invested == 0 ? 0 : yield / invested * 100
}

// shortTime вытаскивает ЧЧ:ММ из RFC3339-метки обновления.
private func shortTime(_ rfc3339: String) -> String {
    let iso = ISO8601DateFormatter()
    iso.formatOptions = [.withInternetDateTime]
    guard let date = iso.date(from: rfc3339) else { return rfc3339 }
    let out = DateFormatter()
    out.locale = Locale(identifier: "ru_RU")
    out.dateFormat = "HH:mm"
    return out.string(from: date)
}

// Запуск как «аксессуар»: без иконки в доке, только в строке меню.
let app = NSApplication.shared
app.setActivationPolicy(.accessory)
let delegate = AppDelegate()
app.delegate = delegate
app.run()
