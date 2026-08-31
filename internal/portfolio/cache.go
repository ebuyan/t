package portfolio

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"tinvest/internal/tinvest"
)

// TTL кешей. Лёгкий срез (GetPortfolio) обновляем часто — он и на странице, и в
// реестре. Тяжёлые метаданные (названия, секторы, дивиденды) меняются редко и
// нужны только квартальному срезу, поэтому обновляются на порядок реже. История
// выплат меняется всего несколько раз в год — ей часа тем более достаточно.
const (
	snapshotTTL  = time.Minute
	metaTTL      = time.Hour
	dividendsTTL = time.Hour
)

// Collector ходит в T-Invest API за срезом и метаданными по инвестиционным счетам.
type Collector struct {
	client *tinvest.Client
}

func NewCollector(client *tinvest.Client) *Collector {
	return &Collector{client: client}
}

// Snapshot собирает лёгкий срез: GetAccounts + GetPortfolio по счетам.
func (c *Collector) Snapshot(ctx context.Context) (*Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	targets, err := c.accounts(ctx)
	if err != nil {
		return nil, err
	}
	return collectSnapshot(ctx, c.client, targets, time.Now())
}

// Dividends собирает полученные за всё время дивиденды по всем счетам
// (GetOperationsByCursor с начала истории, поэтому отдельно от среза).
func (c *Collector) Dividends(ctx context.Context) (tinvest.Dec, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	targets, err := c.accounts(ctx)
	if err != nil {
		return tinvest.Dec{}, err
	}
	return collectDividends(ctx, c.client, targets, time.Now())
}

// accounts возвращает инвестиционные счета, по которым работаем.
func (c *Collector) accounts(ctx context.Context) ([]tinvest.Account, error) {
	all, err := c.client.GetAccounts(ctx)
	if err != nil {
		return nil, err
	}
	targets := selectAccounts(all)
	if len(targets) == 0 {
		return nil, fmt.Errorf("no matching accounts found (available: %d)", len(all))
	}
	return targets, nil
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
	divs    tinvest.Dec
	divsAt  time.Time
	divsOK  bool
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

// Dividends возвращает сумму полученных дивидендов за всё время и время сбора.
// Пока не собраны — ошибку: лучше показать доход без дивидендов, чем нулём.
func (c *Cache) Dividends() (tinvest.Dec, time.Time, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.divsOK {
		return tinvest.Dec{}, time.Time{}, fmt.Errorf("dividends not collected yet")
	}
	return c.divs, c.divsAt, nil
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

// refreshDividends обновляет сумму выплат. Неудачу логирует, прошлую сумму не
// затирает — как и со срезом.
func (c *Cache) refreshDividends(ctx context.Context) {
	d, err := c.col.Dividends(ctx)
	if err != nil {
		slog.ErrorContext(ctx, "dividends refresh failed", slog.Any("error", err))
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.divs, c.divsAt, c.divsOK = d, time.Now(), true
}

// Run обновляет срез каждые snapshotTTL, метаданные и дивиденды — каждые свои
// TTL, пока жив ctx. Сразу на старте собирает всё.
func (c *Cache) Run(ctx context.Context) {
	c.refreshSnapshot(ctx)
	c.refreshMeta(ctx)
	c.refreshDividends(ctx)

	snapT := time.NewTicker(snapshotTTL)
	metaT := time.NewTicker(metaTTL)
	divT := time.NewTicker(dividendsTTL)
	defer snapT.Stop()
	defer metaT.Stop()
	defer divT.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-snapT.C:
			c.refreshSnapshot(ctx)
		case <-metaT.C:
			c.refreshMeta(ctx)
		case <-divT.C:
			c.refreshDividends(ctx)
		}
	}
}
