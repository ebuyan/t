package portfolio

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"tportfolio/internal/tinvest"
)

// TTL кешей. Лёгкий срез (GetPortfolio) обновляем часто — он и на странице, и в
// реестре. Тяжёлые метаданные (названия, секторы, дивиденды) меняются редко и
// нужны только квартальному срезу, поэтому обновляются на порядок реже.
const (
	snapshotTTL = time.Minute
	metaTTL     = time.Hour
)

// Collector ходит в T-Invest API за срезом и метаданными по выбранным счетам.
type Collector struct {
	client   *tinvest.Client
	accounts string // сырой список id из TINVEST_ACCOUNTS
}

func NewCollector(client *tinvest.Client, accounts string) *Collector {
	return &Collector{client: client, accounts: accounts}
}

// Snapshot собирает лёгкий срез: GetAccounts + GetPortfolio по счетам.
func (c *Collector) Snapshot(ctx context.Context) (*Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	all, err := c.client.GetAccounts(ctx)
	if err != nil {
		return nil, err
	}
	targets := selectAccounts(all, c.accounts)
	if len(targets) == 0 {
		return nil, fmt.Errorf("no matching accounts found (available: %d)", len(all))
	}
	return collectSnapshot(ctx, c.client, targets, time.Now())
}

// Meta собирает справку по бумагам среза (дорого: ShareBy + GetDividends на бумагу).
func (c *Collector) Meta(ctx context.Context, holdings []Holding) (*Meta, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	return collectMeta(ctx, c.client, holdings)
}

// Cache хранит последний срез и метаданные. Из среза читают и веб-страница, и
// задачи по расписанию — в API ходит только фоновое обновление.
type Cache struct {
	col *Collector

	mu      sync.RWMutex
	snap    *Snapshot
	snapAt  time.Time
	snapErr error
	meta    *Meta
	metaAt  time.Time
}

func NewCache(col *Collector) *Cache {
	return &Cache{col: col}
}

// Snapshot возвращает последний срез и время его сбора. Пока ни одного успешного
// сбора не было — ошибку (в т.ч. ошибку последней неудачной попытки).
func (c *Cache) Snapshot() (*Snapshot, time.Time, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.snap == nil {
		if c.snapErr != nil {
			return nil, time.Time{}, c.snapErr
		}
		return nil, time.Time{}, fmt.Errorf("snapshot not collected yet")
	}
	return c.snap, c.snapAt, nil
}

// Meta возвращает последние метаданные. Пока не собраны — ошибку.
func (c *Cache) Meta() (*Meta, time.Time, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.meta == nil {
		return nil, time.Time{}, fmt.Errorf("meta not collected yet")
	}
	return c.meta, c.metaAt, nil
}

// refreshSnapshot собирает свежий срез. Неудачу логирует, но прошлый срез не
// затирает: лучше отдать чуть устаревшие данные, чем ошибку.
func (c *Cache) refreshSnapshot(ctx context.Context) {
	s, err := c.col.Snapshot(ctx)

	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		c.snapErr = err
		slog.ErrorContext(ctx, "snapshot refresh failed", slog.Any("error", err))
		return
	}
	c.snap, c.snapAt, c.snapErr = s, time.Now(), nil
}

// refreshMeta обновляет метаданные по бумагам последнего среза. Без среза
// пропускает попытку — соберёт в следующий раз, когда срез появится.
func (c *Cache) refreshMeta(ctx context.Context) {
	s, _, err := c.Snapshot()
	if err != nil {
		return
	}
	m, err := c.col.Meta(ctx, s.Holdings)
	if err != nil {
		slog.ErrorContext(ctx, "meta refresh failed", slog.Any("error", err))
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.meta, c.metaAt = m, time.Now()
}

// Run обновляет срез каждые snapshotTTL, а метаданные — каждые metaTTL, пока жив
// ctx. Сразу на старте собирает и то, и другое.
func (c *Cache) Run(ctx context.Context) {
	c.refreshSnapshot(ctx)
	c.refreshMeta(ctx)

	snapT := time.NewTicker(snapshotTTL)
	metaT := time.NewTicker(metaTTL)
	defer snapT.Stop()
	defer metaT.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-snapT.C:
			c.refreshSnapshot(ctx)
		case <-metaT.C:
			c.refreshMeta(ctx)
		}
	}
}
