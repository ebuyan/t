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

	"tinvest/internal/portfolio"
	"tinvest/internal/tinvest"
	"tinvest/web"
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

// num сериализует Dec в JSON-число без float: деньги нигде не проходят через float.
func num(d tinvest.Dec) json.Number { return json.Number(d.String(2)) }

// assetJSON — класс активов в сводке: стоимость и доход за всё время (в рублях).
type assetJSON struct {
	Value json.Number `json:"value"`
	Yield json.Number `json:"yield"`
}

// holdingJSON — одна бумага в составе: тикер, название (если известно), стоимость
// и изменение за сегодня (в рублях).
type holdingJSON struct {
	Ticker    string      `json:"ticker"`
	Name      string      `json:"name,omitempty"`
	Value     json.Number `json:"value"`
	DayChange json.Number `json:"day_change"`
}

// todayResponse — сводка «всего за сегодня» для виджета. Все суммы в рублях,
// day_change_pct — в процентах. Проценты долей и доходности виджет считает сам.
type todayResponse struct {
	PortfolioValue json.Number   `json:"portfolio_value"` // полная стоимость (с кэшем и облигациями)
	Total          json.Number   `json:"total"`           // база долей: акции + золото
	DayChange      json.Number   `json:"day_change"`
	DayChangePct   json.Number   `json:"day_change_pct"`
	Income         json.Number   `json:"income"` // доход за всё время (акции + золото)
	Shares         assetJSON     `json:"shares"`
	Gold           assetJSON     `json:"gold"`
	Cash           json.Number   `json:"cash"`
	Holdings       []holdingJSON `json:"holdings"`
	Updated        string        `json:"updated"`
}

// handleAPIToday отдаёт JSON-сводку из кеша: полная стоимость портфеля, изменение
// за сегодня, доход за всё время, разбивка по классам и состав. Без авторизации,
// как и страница; источник — тот же лёгкий срез (названия бумаг — из часового кеша).
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
		// Названия по возможности; их отсутствие не мешает отдать сводку.
		m, _, _ := cfg.Cache.Meta()

		resp := todayResponse{
			PortfolioValue: num(s.PortfolioValue),
			Total:          num(s.Total),
			DayChange:      num(s.DayChange),
			DayChangePct:   num(s.DayChangePct),
			Income:         num(s.StockYield.Add(s.GoldYield)),
			Shares:         assetJSON{Value: num(s.Shares), Yield: num(s.StockYield)},
			Gold:           assetJSON{Value: num(s.Gold), Yield: num(s.GoldYield)},
			Cash:           num(s.Cash),
			Holdings:       make([]holdingJSON, 0, len(s.Holdings)),
			Updated:        updated.Format(time.RFC3339),
		}
		for _, h := range s.Holdings {
			name := ""
			if m != nil {
				name = m.Names[h.UID]
			}
			resp.Holdings = append(resp.Holdings, holdingJSON{
				Ticker:    h.Ticker,
				Name:      name,
				Value:     num(h.Value),
				DayChange: num(h.DayChange),
			})
		}
		// Золото — отдельной строкой состава, как в таблице на странице.
		if !s.Gold.IsZero() {
			resp.Holdings = append(resp.Holdings, holdingJSON{
				Ticker:    "GLDRUB_TOM",
				Name:      "Золото",
				Value:     num(s.Gold),
				DayChange: num(s.GoldDayChange),
			})
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
