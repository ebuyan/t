// Package server поднимает веб-сервер страницы доходности. Данные берёт из кеша
// (пакет portfolio), шаблоны — из пакета web.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"tportfolio/internal/portfolio"
	"tportfolio/web"
)

// Config — зависимости веб-сервера.
type Config struct {
	Addr  string
	Cache *portfolio.Cache
	// SyncRegistry записывает текущий срез в реестр доходности. nil — кнопка
	// синхронизации на странице недоступна (реестр не сконфигурирован).
	SyncRegistry func(context.Context, *portfolio.Snapshot) error
}

// Serve поднимает сервер на cfg.Addr и работает, пока жив ctx.
func Serve(ctx context.Context, cfg Config) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", handleIndex(cfg))
	mux.HandleFunc("/api/today", handleAPIToday(cfg))
	mux.HandleFunc("/sync/registry", handleSyncRegistry(cfg))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	//nolint:gosec // shutdownOnDone намеренно берёт свежий контекст: ctx к тому моменту уже отменён
	go shutdownOnDone(ctx, srv)

	slog.InfoContext(ctx, "http server listening", slog.String("addr", cfg.Addr))
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.ErrorContext(ctx, "http server error", slog.Any("error", err))
	}
}

// shutdownOnDone гасит сервер, когда отменяют ctx. Контекст завершения намеренно
// не наследует ctx: тот уже отменён, а на graceful-shutdown нужен свежий дедлайн.
func shutdownOnDone(ctx context.Context, srv *http.Server) {
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	//nolint:contextcheck // ctx уже отменён; на завершение нужен свежий контекст
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.ErrorContext(shutdownCtx, "http shutdown error", slog.Any("error", err))
	}
}

func handleIndex(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		s, updated, err := cfg.Cache.Snapshot()
		if err != nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = web.Error.Execute(w, err.Error())
			return
		}
		// Метаданные (названия бумаг) — по возможности; их отсутствие не мешает
		// показать доходность.
		m, _, _ := cfg.Cache.Meta()

		view := portfolio.BuildYieldView(s, m, updated)
		view.CanSync = cfg.SyncRegistry != nil

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := web.Page.Execute(w, view); err != nil {
			slog.ErrorContext(r.Context(), "render page error", slog.Any("error", err))
		}
	}
}

// todayResponse — сводка «всего за сегодня» для виджета. Числа отдаём как
// json.Number из Dec.String, без float: деньги нигде не проходят через float.
// portfolio_value и day_change — в рублях, day_change_pct — в процентах.
type todayResponse struct {
	PortfolioValue json.Number `json:"portfolio_value"`
	DayChange      json.Number `json:"day_change"`
	DayChangePct   json.Number `json:"day_change_pct"`
	Updated        string      `json:"updated"`
}

// handleAPIToday отдаёт JSON-сводку из кеша: полная стоимость портфеля и изменение
// за сегодня. Без авторизации, как и страница; источник — тот же лёгкий срез.
func handleAPIToday(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s, updated, err := cfg.Cache.Snapshot()
		if err != nil {
			http.Error(w, "data not ready: "+err.Error(), http.StatusServiceUnavailable)
			return
		}
		resp := todayResponse{
			PortfolioValue: json.Number(s.PortfolioValue.String(2)),
			DayChange:      json.Number(s.DayChange.String(2)),
			DayChangePct:   json.Number(s.DayChangePct.String(2)),
			Updated:        updated.Format(time.RFC3339),
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			slog.ErrorContext(r.Context(), "render api today error", slog.Any("error", err))
		}
	}
}

// handleSyncRegistry по кнопке на странице дописывает текущий срез в реестр.
func handleSyncRegistry(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if cfg.SyncRegistry == nil {
			http.Error(w, "registry file is not configured (TINVEST_REGISTRY_FILE)", http.StatusBadRequest)
			return
		}
		s, _, err := cfg.Cache.Snapshot()
		if err != nil {
			http.Error(w, "data not ready: "+err.Error(), http.StatusServiceUnavailable)
			return
		}
		if err := cfg.SyncRegistry(r.Context(), s); err != nil {
			slog.ErrorContext(r.Context(), "registry sync error", slog.Any("error", err))
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte("written to registry for " + s.Date.Format("2006-01-02")))
	}
}
