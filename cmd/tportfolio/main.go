package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
	_ "time/tzdata"

	"tportfolio/internal/config"
	"tportfolio/internal/portfolio"
	"tportfolio/internal/server"
	"tportfolio/internal/tinvest"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("fatal error", slog.Any("error", err))
		os.Exit(1)
	}
}

// run поднимает сервис и возвращает ошибку старта. Вынесено из main, чтобы os.Exit
// не проглатывал defer stop().
func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	client := tinvest.NewClient(ctx, cfg.Token)
	cache := portfolio.NewCache(portfolio.NewCollector(client, cfg.Accounts))

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		cache.Run(ctx)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		server.Serve(ctx, server.Config{
			Addr:         cfg.HTTPAddr,
			Cache:        cache,
			SyncRegistry: registrySync(cfg.RegistryFile, cfg.PortfolioFile),
		})
	}()

	if cfg.RegistrySchedule != "" && cfg.RegistryFile != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			startRegistrySchedule(ctx, cache, cfg.RegistrySchedule, cfg.RegistryFile, cfg.PortfolioFile)
		}()
	}

	if cfg.PortfolioSchedule != "" && cfg.PortfolioFile != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			startPortfolioSchedule(ctx, cache, cfg.PortfolioSchedule, cfg.PortfolioFile)
		}()
	}

	wg.Wait()
	slog.InfoContext(ctx, "stopped by signal")
	return nil
}

// registrySync возвращает функцию записи среза в реестр или nil, если реестр не
// сконфигурирован (тогда кнопка синхронизации на странице скрыта). Если задан файл
// долей, заодно обновляет бары прогресса в нём.
func registrySync(registryFile, portfolioFile string) func(context.Context, *portfolio.Snapshot) error {
	if registryFile == "" {
		return nil
	}
	return func(ctx context.Context, s *portfolio.Snapshot) error {
		return writeRegistry(ctx, s, registryFile, portfolioFile)
	}
}

// writeRegistry дописывает срез в реестр и, если задан файл долей, пересчитывает в
// нём бары прогресса. Общая точка для расписания и кнопки на странице.
func writeRegistry(ctx context.Context, s *portfolio.Snapshot, registryFile, portfolioFile string) error {
	if err := portfolio.UpdateRegistryFile(ctx, registryFile, s); err != nil {
		return err
	}
	if portfolioFile != "" {
		if err := portfolio.UpdateProgressBarsFile(ctx, portfolioFile, s); err != nil {
			return err
		}
	}
	return nil
}

func startRegistrySchedule(ctx context.Context, c *portfolio.Cache, schCfg, registryFile, portfolioFile string) {
	sch, err := portfolio.ParseSchedule(schCfg)
	if err != nil {
		slog.ErrorContext(ctx, "invalid registry schedule", slog.Any("error", err))
		return
	}
	loop(ctx, "registry", sch.Next, func() error {
		s, _, err := c.Snapshot()
		if err != nil {
			return err
		}
		return writeRegistry(ctx, s, registryFile, portfolioFile)
	})
}

func startPortfolioSchedule(ctx context.Context, c *portfolio.Cache, schCfg, file string) {
	sch, err := portfolio.ParseSchedule(schCfg)
	if err != nil {
		slog.ErrorContext(ctx, "invalid portfolio schedule", slog.Any("error", err))
		return
	}

	q := portfolio.Quarterly{At: sch.At}
	loop(ctx, "portfolio slice", q.Next, func() error {
		s, _, err := c.Snapshot()
		if err != nil {
			return err
		}
		m, _, err := c.Meta()
		if err != nil {
			return err
		}

		slog.InfoContext(ctx, "portfolio slice collected",
			slog.String("date", s.ColumnDate()),
			slog.String("shares", s.Shares.String(2)),
			slog.String("gold", s.Gold.String(2)),
			slog.String("total", s.Total.String(2)),
		)

		return portfolio.UpdatePortfolioFile(ctx, file, s, m)
	})
}

// loop ждёт следующего срабатывания и выполняет задачу. Ошибка логируется, но не
// роняет процесс: иначе restart: unless-stopped увёл бы контейнер в цикл
// перезапусков.
func loop(ctx context.Context, name string, next func(time.Time) time.Time, task func() error) {
	for {
		fire := next(time.Now())
		slog.InfoContext(ctx, "task scheduled",
			slog.String("task", name),
			slog.Time("next_run", fire),
		)

		timer := time.NewTimer(time.Until(fire))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}

		if err := task(); err != nil {
			slog.ErrorContext(ctx, "task failed", slog.String("task", name), slog.Any("error", err))
		}
	}
}
