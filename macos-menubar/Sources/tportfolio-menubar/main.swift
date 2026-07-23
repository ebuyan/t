import AppKit

// Меню-бар со сводкой «всего за сегодня» из tportfolio.
// Адрес сервиса берётся из переменной окружения TPORTFOLIO_URL, иначе — из
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
    let ticker: String
    let name: String?
    let value: Double
    let dayChange: Double

    enum CodingKeys: String, CodingKey {
        case ticker, name, value
        case dayChange = "day_change"
    }
}

struct Today: Decodable {
    let portfolioValue: Double
    let total: Double
    let dayChange: Double
    let dayChangePct: Double
    let income: Double
    let shares: Asset
    let gold: Asset
    let cash: Double
    let holdings: [Holding]
    let updated: String

    enum CodingKeys: String, CodingKey {
        case portfolioValue = "portfolio_value"
        case total, income, shares, gold, cash, holdings, updated
        case dayChange = "day_change"
        case dayChangePct = "day_change_pct"
    }
}

final class AppDelegate: NSObject, NSApplicationDelegate {
    private var statusItem: NSStatusItem!
    private var timer: Timer?
    private let endpoint: URL

    override init() {
        let raw = ProcessInfo.processInfo.environment["TPORTFOLIO_URL"] ?? defaultURL
        guard let url = URL(string: raw) else {
            fatalError("invalid TPORTFOLIO_URL: \(raw)")
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
                DispatchQueue.main.async { self.render(today) }
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

        let menu = NSMenu()
        menu.addItem(info("Стоимость портфеля", rub(t.portfolioValue)))
        menu.addItem(colored("За сегодня", "\(signedRub(t.dayChange)) (\(signedPct(t.dayChangePct)))", t.dayChange))
        menu.addItem(colored("Доход за всё время", "\(signedRub(t.income)) (\(signedPct(yieldPct(t.total, t.income))))", t.income))

        menu.addItem(.separator())
        menu.addItem(assetItem("Акции", t.shares, base: t.total))
        menu.addItem(assetItem("Золото", t.gold, base: t.total))
        if t.cash != 0 {
            menu.addItem(info("Кеш", rub(t.cash)))
        }

        if !t.holdings.isEmpty {
            menu.addItem(.separator())
            menu.addItem(holdingsSubmenu(t.holdings))
        }

        menu.addItem(.separator())
        menu.addItem(info("Обновлено", shortTime(t.updated)))
        menu.addItem(withKey("Обновить сейчас", #selector(refreshNow), "r"))
        menu.addItem(withKey("Выход", #selector(quit), "q"))
        statusItem.menu = menu
    }

    private func showError(_ msg: String) {
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

    // assetItem — строка класса активов: стоимость, доля от базы и доходность.
    private func assetItem(_ name: String, _ a: Asset, base: Double) -> NSMenuItem {
        let line = "\(rub(a.value)) · \(pct(pctOf(a.value, base))) · \(signedPct(yieldPct(a.value, a.yield)))"
        return colored(name, line, a.yield)
    }

    // holdingsSubmenu — подменю «Состав»: бумаги с изменением за сегодня.
    private func holdingsSubmenu(_ holdings: [Holding]) -> NSMenuItem {
        let root = NSMenuItem(title: "Состав (\(holdings.count))", action: nil, keyEquivalent: "")
        let sub = NSMenu()
        for h in holdings {
            let label = h.name.map { "\(h.ticker) — \($0)" } ?? h.ticker
            let line = "\(rub(h.value)) · \(signedRub(h.dayChange))"
            sub.addItem(colored(label, line, h.dayChange))
        }
        root.submenu = sub
        return root
    }

    private func placeholderMenu(_ title: String) -> NSMenu {
        let menu = NSMenu()
        menu.addItem(NSMenuItem(title: title, action: nil, keyEquivalent: ""))
        menu.addItem(.separator())
        return menu
    }

    // info — «Метка: значение» серой строкой (без действия).
    private func info(_ label: String, _ value: String) -> NSMenuItem {
        NSMenuItem(title: "\(label): \(value)", action: nil, keyEquivalent: "")
    }

    // colored — «Метка: значение», где значение окрашено по знаку sign.
    private func colored(_ label: String, _ value: String, _ sign: Double) -> NSMenuItem {
        let item = NSMenuItem(title: "\(label): \(value)", action: nil, keyEquivalent: "")
        let s = NSMutableAttributedString(string: "\(label): ")
        let color: NSColor = sign >= 0 ? .systemGreen : .systemRed
        s.append(NSAttributedString(string: value, attributes: [.foregroundColor: color]))
        item.attributedTitle = s
        return item
    }

    private func withKey(_ title: String, _ action: Selector, _ key: String) -> NSMenuItem {
        NSMenuItem(title: title, action: action, keyEquivalent: key)
    }
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
