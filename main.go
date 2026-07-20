package main

import (
	"context"
	"fmt"
	"log"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
	_ "time/tzdata" // база часовых поясов внутрь бинарника: в контейнере её может не быть

	"github.com/kelseyhightower/envconfig"
)

type config struct {
	Token        string `envconfig:"TINVEST_TOKEN"         required:"true" default:""`
	Accounts     string `envconfig:"TINVEST_ACCOUNTS"`
	ListAccounts bool   `envconfig:"TINVEST_LIST_ACCOUNTS" default:"false"`

	// RegistryFile — реестр доходности в формате inline-полей Dataview.
	RegistryFile string `envconfig:"TINVEST_REGISTRY_FILE"`
	// RegistrySchedule — когда дописывать запись: «Mon,Fri 11:00» или «11:00»
	// для ежедневного запуска. Пусто — не обновлять.
	RegistrySchedule string `envconfig:"TINVEST_REGISTRY_SCHEDULE"`

	// PortfolioFile — файл Обсидиана с таблицами долей портфеля.
	PortfolioFile string `envconfig:"TINVEST_PORTFOLIO_FILE"`
	// PortfolioSchedule — время HH:MM, в которое обновлять PortfolioFile
	// первого числа каждого квартала. Пусто — не обновлять.
	PortfolioSchedule string `envconfig:"TINVEST_PORTFOLIO_SCHEDULE"`

	// Now — выполнить обе задачи сразу и выйти, не дожидаясь расписания.
	Now bool `envconfig:"TINVEST_NOW" default:"false"`

	// Backup — класть копию файла рядом перед каждой записью. Обсидиан такие
	// файлы не показывает (расширение не .md), но они синкаются и копятся,
	// поэтому после обкатки достаточно выставить false.
	Backup bool `envconfig:"TINVEST_BACKUP" default:"true"`
}

var cfg config

func loadConfig() error {
	return envconfig.Process("", &cfg)
}

func main() {
	if err := loadConfig(); err != nil {
		log.Fatalf("конфиг: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	switch {
	case cfg.ListAccounts:
		if err := listAccounts(ctx); err != nil {
			log.Fatalf("ошибка: %v", err)
		}
	case cfg.Now || (cfg.RegistrySchedule == "" && cfg.PortfolioSchedule == ""):
		if err := runOnce(ctx); err != nil {
			log.Fatalf("ошибка: %v", err)
		}
	default:
		serve(ctx)
	}
}

// serve держит процесс живым. Реестр доходности и квартальный срез долей идут
// независимыми горутинами: они ходят в разные методы API и с разной частотой,
// и затянувшийся срез не должен сдвигать запись в реестр.
func serve(ctx context.Context) {
	var wg sync.WaitGroup

	if cfg.RegistrySchedule != "" {
		if cfg.RegistryFile == "" {
			log.Fatal("конфиг: задан TINVEST_REGISTRY_SCHEDULE, но не задан TINVEST_REGISTRY_FILE")
		}
		sch, err := parseSchedule(cfg.RegistrySchedule)
		if err != nil {
			log.Fatalf("конфиг: TINVEST_REGISTRY_SCHEDULE: %v", err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			loop(ctx, "реестр доходности", sch.next, func() error { return runRegistry(ctx) })
		}()
	}

	if cfg.PortfolioSchedule != "" {
		if cfg.PortfolioFile == "" {
			log.Fatal("конфиг: задан TINVEST_PORTFOLIO_SCHEDULE, но не задан TINVEST_PORTFOLIO_FILE")
		}
		sch, err := parseSchedule(cfg.PortfolioSchedule)
		if err != nil {
			log.Fatalf("конфиг: TINVEST_PORTFOLIO_SCHEDULE: %v", err)
		}
		q := quarterly{at: sch.at}
		wg.Add(1)
		go func() {
			defer wg.Done()
			loop(ctx, "срез долей", q.next, func() error { return runPortfolio(ctx) })
		}()
	}

	wg.Wait()
	log.Print("остановка по сигналу")
}

// loop ждёт следующего срабатывания и выполняет задачу. Ошибка логируется, но не
// роняет процесс: иначе restart: unless-stopped увёл бы контейнер в цикл
// перезапусков.
func loop(ctx context.Context, name string, next func(time.Time) time.Time, task func() error) {
	for {
		fire := next(time.Now())
		log.Printf("%s — следующий запуск: %s", name, fire.Format("2006-01-02 15:04 MST"))

		timer := time.NewTimer(time.Until(fire))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		if err := task(); err != nil {
			log.Printf("%s: %v", name, err)
		}
	}
}

// runOnce выполняет обе задачи разово — для ручного прогона и отладки.
func runOnce(ctx context.Context) error {
	if cfg.RegistryFile != "" {
		if err := runRegistry(ctx); err != nil {
			return err
		}
	}
	if cfg.PortfolioFile != "" {
		if err := runPortfolio(ctx); err != nil {
			return err
		}
	}
	if cfg.RegistryFile == "" && cfg.PortfolioFile == "" {
		return fmt.Errorf("нечего делать: не задан ни TINVEST_REGISTRY_FILE, ни TINVEST_PORTFOLIO_FILE")
	}
	return nil
}

// runRegistry дописывает запись о текущей доходности в начало реестра.
func runRegistry(ctx context.Context) error {
	if cfg.RegistryFile == "" {
		return fmt.Errorf("не задан TINVEST_REGISTRY_FILE")
	}
	s, err := snapshotNow(ctx)
	if err != nil {
		return err
	}
	return updateRegistryFile(cfg.RegistryFile, s)
}

// runPortfolio собирает срез долей и дописывает столбец в файл Обсидиана.
func runPortfolio(ctx context.Context) error {
	if cfg.PortfolioFile == "" {
		return fmt.Errorf("не задан TINVEST_PORTFOLIO_FILE")
	}
	s, err := snapshotNow(ctx)
	if err != nil {
		return err
	}
	log.Printf("срез на %s: акции %s ₽, золото %s ₽, всего %s ₽",
		s.columnDate(), s.shares.String(2), s.gold.String(2), s.total.String(2))

	return updatePortfolioFile(cfg.PortfolioFile, s)
}

// snapshotNow собирает срез портфеля по выбранным счетам.
func snapshotNow(ctx context.Context) (*snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	client := NewClient(cfg.Token)
	all, err := client.GetAccounts(ctx)
	if err != nil {
		return nil, err
	}
	targets := selectAccounts(all, cfg.Accounts)
	if len(targets) == 0 {
		return nil, fmt.Errorf("не найдено подходящих счетов (всего доступно: %d)", len(all))
	}
	return collectSnapshot(ctx, client, targets, time.Now())
}

func listAccounts(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	all, err := NewClient(cfg.Token).GetAccounts(ctx)
	if err != nil {
		return err
	}
	for _, a := range all {
		fmt.Printf("%s\t%s\t%s\t%s\n", a.ID, a.Type, a.Status, a.Name)
	}
	return nil
}

// selectAccounts отбирает счета по явному списку id, иначе — все инвестиционные.
func selectAccounts(all []Account, ids string) []Account {
	if strings.TrimSpace(ids) != "" {
		want := make(map[string]bool)
		for _, id := range strings.Split(ids, ",") {
			if id = strings.TrimSpace(id); id != "" {
				want[id] = true
			}
		}
		var res []Account
		for _, a := range all {
			if want[a.ID] {
				res = append(res, a)
			}
		}
		return res
	}

	var res []Account
	for _, a := range all {
		switch a.Type {
		case "ACCOUNT_TYPE_TINKOFF", "ACCOUNT_TYPE_TINKOFF_IIS":
			res = append(res, a)
		}
	}
	return res
}

func accountLabel(a Account) string {
	if a.Name != "" {
		return a.Name
	}
	return a.ID
}
