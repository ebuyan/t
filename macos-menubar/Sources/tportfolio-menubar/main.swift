import AppKit

// Меню-бар со сводкой «всего за сегодня» из tportfolio.
// Адрес сервиса берётся из переменной окружения TPORTFOLIO_URL, иначе — из
// значения по умолчанию ниже. Замени IP на свой хост со стеком ha.
let defaultURL = "http://192.168.0.108:8077/api/today"

// Как часто опрашивать сервис. Данные на сервере обновляются раз в минуту.
let refreshInterval: TimeInterval = 60

// today — сводка из /api/today. Числа приходят как JSON-числа (в рублях; pct — %).
struct Today: Decodable {
    let portfolioValue: Double
    let dayChange: Double
    let dayChangePct: Double
    let updated: String

    enum CodingKeys: String, CodingKey {
        case portfolioValue = "portfolio_value"
        case dayChange = "day_change"
        case dayChangePct = "day_change_pct"
        case updated
    }
}

final class AppDelegate: NSObject, NSApplicationDelegate {
    private var statusItem: NSStatusItem!
    private var timer: Timer?
    private let endpoint: URL

    // Пункты меню, которые обновляем данными.
    private let valueItem = NSMenuItem(title: "Стоимость: —", action: nil, keyEquivalent: "")
    private let changeItem = NSMenuItem(title: "За сегодня: —", action: nil, keyEquivalent: "")
    private let updatedItem = NSMenuItem(title: "Обновлено: —", action: nil, keyEquivalent: "")

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

        let menu = NSMenu()
        menu.addItem(valueItem)
        menu.addItem(changeItem)
        menu.addItem(updatedItem)
        menu.addItem(.separator())
        menu.addItem(NSMenuItem(title: "Обновить сейчас", action: #selector(refreshNow), keyEquivalent: "r"))
        menu.addItem(NSMenuItem(title: "Выход", action: #selector(quit), keyEquivalent: "q"))
        statusItem.menu = menu

        fetch()
        timer = Timer.scheduledTimer(withTimeInterval: refreshInterval, repeats: true) { [weak self] _ in
            self?.fetch()
        }
    }

    @objc private func refreshNow() { fetch() }

    @objc private func quit() { NSApplication.shared.terminate(nil) }

    // fetch дёргает /api/today и раскладывает результат по строке меню и пунктам.
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

    // render печатает сводку. В строке меню — только изменение за день (цветом),
    // подробности — в выпадашке.
    private func render(_ t: Today) {
        let up = t.dayChange >= 0
        let color: NSColor = up ? .systemGreen : .systemRed
        let arrow = up ? "▲" : "▼"
        let title = "\(arrow) \(signedRub(t.dayChange))"
        statusItem.button?.attributedTitle = NSAttributedString(
            string: title,
            attributes: [.foregroundColor: color]
        )

        valueItem.title = "Стоимость: \(rub(t.portfolioValue))"
        changeItem.title = "За сегодня: \(signedRub(t.dayChange)) (\(signedPct(t.dayChangePct)))"
        updatedItem.title = "Обновлено: \(shortTime(t.updated))"
    }

    private func showError(_ msg: String) {
        statusItem.button?.attributedTitle = NSAttributedString(
            string: "⚠︎",
            attributes: [.foregroundColor: NSColor.systemOrange]
        )
        updatedItem.title = "Ошибка: \(msg)"
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
    let sign = v > 0 ? "+" : ""
    return sign + rub(v)
}

private func signedPct(_ v: Double) -> String {
    let sign = v > 0 ? "+" : ""
    let s = pctFormatter.string(from: NSNumber(value: v)) ?? "\(v)"
    return "\(sign)\(s)%"
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
