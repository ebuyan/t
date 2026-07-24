# tinvest-menubar

Меню-бар для macOS со сводкой «всего за сегодня» из сервиса `tinvest`.
В строке меню — изменение портфеля за день (зелёным/красным), в выпадашке —
полная стоимость, изменение в ₽ и %, время последнего обновления.

Данные берутся из ручки `GET /api/today` сервиса `tinvest` (см. корневой
`README.md`). Опрос — раз в минуту.

Полноценный **Xcode не нужен** — хватает Command Line Tools (`swift`). Приложение
для личного использования **подпись Apple не требует**: свой же локально собранный
бинарник запускается без Gatekeeper.

## 1. Разовая проверка

```sh
# при необходимости: xcode-select --install
cd macos-menubar
TINVEST_URL="http://192.168.0.108:8077/api/today" swift run
```

В строке меню появится сводка. Замени IP на адрес своего хоста со стеком `ha`
(порт `8077` должен быть опубликован — см. корневой `README.md`). Если не задать
`TINVEST_URL`, берётся значение по умолчанию из `main.swift`.

Остановить: пункт **«Выход»** в меню или `Ctrl+C` в терминале.

## 2. Сборка релиза

```sh
cd macos-menubar
swift build -c release
# готовый бинарник:
#   .build/release/tinvest-menubar
```

Скопируй его в постоянное место, чтобы не зависеть от каталога сборки:

```sh
mkdir -p ~/Applications
cp .build/release/tinvest-menubar ~/Applications/tinvest-menubar
```

## 3. Автозапуск при логине (LaunchAgent)

Отредактируй `LaunchAgent.plist` (путь к бинарнику и свой `TINVEST_URL`),
затем:

```sh
cp LaunchAgent.plist ~/Library/LaunchAgents/com.ebuyan.tinvest-menubar.plist
launchctl load ~/Library/LaunchAgents/com.ebuyan.tinvest-menubar.plist
```

Выгрузить (отключить автозапуск):

```sh
launchctl unload ~/Library/LaunchAgents/com.ebuyan.tinvest-menubar.plist
```

## Настройки

- `TINVEST_URL` — адрес ручки `/api/today` (переменная окружения; для
  LaunchAgent задаётся в plist).
- Интервал опроса и адрес по умолчанию — константы в начале `main.swift`.
