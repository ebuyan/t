package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"tportfolio/internal/portfolio"
)

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
