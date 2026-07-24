package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"tinvest/internal/portfolio"
)

func TestAPITodayMethodNotAllowed(t *testing.T) {
	h := handleAPIToday(Config{Cache: portfolio.NewCache(nil)})
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/today", http.NoBody))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("код = %d, хотим %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestAPITodayNoData(t *testing.T) {
	// Кеш пуст — отдаём 503, а не пустой/нулевой JSON.
	h := handleAPIToday(Config{Cache: portfolio.NewCache(nil)})
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/today", http.NoBody))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("код = %d, хотим %d", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestSyncRegistryMethodNotAllowed(t *testing.T) {
	h := handleSyncRegistry(Config{Cache: portfolio.NewCache(nil)})
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/sync/registry", http.NoBody))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("код = %d, хотим %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestSyncRegistryNotConfigured(t *testing.T) {
	// SyncRegistry == nil — реестр не задан, синхронизация недоступна.
	h := handleSyncRegistry(Config{Cache: portfolio.NewCache(nil)})
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/sync/registry", http.NoBody))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("код = %d, хотим %d", rec.Code, http.StatusBadRequest)
	}
}

func TestSyncRegistryNoData(t *testing.T) {
	// Реестр задан, но кеш пуст — отдаём 503, а не пишем в файл.
	called := false
	h := handleSyncRegistry(Config{
		Cache:        portfolio.NewCache(nil),
		SyncRegistry: func(context.Context, *portfolio.Snapshot) error { called = true; return nil },
	})
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/sync/registry", http.NoBody))

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("код = %d, хотим %d", rec.Code, http.StatusServiceUnavailable)
	}
	if called {
		t.Error("SyncRegistry не должен вызываться без собранного среза")
	}
}
