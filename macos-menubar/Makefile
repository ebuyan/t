.PHONY: build release run install autostart autostart-restart autostart-stop status

# Адрес ручки /api/today. Переопределяется: make run URL=http://…:8077/api/today
URL    ?= http://192.168.0.108:8077/api/today

BIN    := $(HOME)/Applications/tinvest-menubar
LABEL  := com.ebuyan.tinvest-menubar
PLIST  := $(HOME)/Library/LaunchAgents/$(LABEL).plist
DOMAIN := gui/$(shell id -u)

build:                     ## debug-сборка (проверка компиляции)
	swift build

release:                   ## релиз-сборка
	swift build -c release

run:                       ## запустить локально в терминале (для проверки)
	TINVEST_URL="$(URL)" swift run

install: release           ## собрать релиз и разложить бинарник в ~/Applications
	mkdir -p $(HOME)/Applications
	cp .build/release/tinvest-menubar $(BIN)

autostart: install         ## установить/обновить LaunchAgent и запустить
	mkdir -p $(HOME)/Library/LaunchAgents
	cp LaunchAgent.plist $(PLIST)
	launchctl bootout $(DOMAIN)/$(LABEL) 2>/dev/null || true
	launchctl bootstrap $(DOMAIN) $(PLIST)
	launchctl kickstart -k $(DOMAIN)/$(LABEL)

autostart-restart: install ## пересобрать релиз и перезапустить уже настроенный автозапуск
	launchctl kickstart -k $(DOMAIN)/$(LABEL)

autostart-stop:            ## остановить автозапуск и снять регистрацию
	launchctl bootout $(DOMAIN)/$(LABEL) 2>/dev/null || true

status:                    ## статус службы автозапуска
	@launchctl print $(DOMAIN)/$(LABEL) 2>/dev/null | grep -E "state =|pid =|program =" || echo "not installed"
