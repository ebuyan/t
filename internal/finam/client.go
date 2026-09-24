// Package finam — минимальный клиент REST-обёртки Finam Trade API
// (https://api.finam.ru): только чтение счёта, истории транзакций и справки по
// инструменту. Контракт взят из официального swagger/proto
// (github.com/FinamWeb/finam-trade-api): поля в snake_case, числа — строками
// google.type.Decimal ({"value": "..."}) и google.type.Money.
package finam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const defaultBaseURL = "https://api.finam.ru"

// jwtTTL — сколько переиспользуем JWT. Финам выдаёт его на 15 минут; обновляем с
// запасом, чтобы запрос не упёрся в истечение посреди сбора среза.
const jwtTTL = 10 * time.Minute

// errUnauthorized — JWT отвергнут (истёк раньше срока или отозван): его надо
// перевыпустить и повторить запрос.
var errUnauthorized = errors.New("unauthorized")

// Client ходит в Finam Trade API. Секрет из личного кабинета («Токены») живёт
// долго, а в запросы идёт короткоживущий JWT, который клиент выпускает сам.
type Client struct {
	secret  string
	baseURL string
	http    *http.Client

	mu    sync.Mutex
	jwt   string
	jwtAt time.Time
}

// Option настраивает клиент.
type Option func(*Client)

// WithBaseURL подменяет адрес API — для тестов на httptest-сервере.
func WithBaseURL(u string) Option {
	return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") }
}

// NewClient создаёт клиент по секрету API. Сертификат api.finam.ru выпущен
// публичным CA, поэтому хватает системного хранилища. REST-шлюз Финама требует
// HTTP/2 — у стандартного транспорта он включён (ForceAttemptHTTP2).
func NewClient(secret string, opts ...Option) *Client {
	rt := http.DefaultTransport
	if tr, ok := rt.(*http.Transport); ok {
		h2 := tr.Clone()
		h2.ForceAttemptHTTP2 = true
		rt = h2
	}
	c := &Client{
		secret:  secret,
		baseURL: defaultBaseURL,
		http:    &http.Client{Timeout: 30 * time.Second, Transport: rt},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// token возвращает действующий JWT, при необходимости выпуская новый.
func (c *Client) token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.jwt != "" && time.Since(c.jwtAt) < jwtTTL {
		return c.jwt, nil
	}

	var resp struct {
		Token string `json:"token"`
	}
	// AuthService вызывается без заголовка Authorization: секрет идёт в теле.
	req := map[string]string{"secret": c.secret}
	if err := c.post(ctx, "/v1/sessions", req, &resp); err != nil {
		return "", fmt.Errorf("auth: %w", err)
	}
	if resp.Token == "" {
		return "", fmt.Errorf("auth: empty token in response")
	}
	c.jwt, c.jwtAt = resp.Token, time.Now()
	return c.jwt, nil
}

// dropToken забывает JWT, чтобы следующий запрос выпустил новый.
func (c *Client) dropToken() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.jwt = ""
}

// AccountIDs возвращает счета, к которым у токена есть доступ (TokenDetails).
func (c *Client) AccountIDs(ctx context.Context) ([]string, error) {
	jwt, err := c.token(ctx)
	if err != nil {
		return nil, err
	}
	var resp struct {
		AccountIDs []string `json:"account_ids"`
	}
	// Как и Auth, TokenDetails не принимает заголовок Authorization — JWT в теле.
	if err := c.post(ctx, "/v1/sessions/details", map[string]string{"token": jwt}, &resp); err != nil {
		return nil, fmt.Errorf("token details: %w", err)
	}
	return resp.AccountIDs, nil
}

// GetAccount возвращает состояние счёта: оценку, деньги и позиции.
func (c *Client) GetAccount(ctx context.Context, accountID string) (*Account, error) {
	var resp Account
	if err := c.get(ctx, "/v1/accounts/"+url.PathEscape(accountID), nil, &resp); err != nil {
		return nil, fmt.Errorf("get account %s: %w", accountID, err)
	}
	return &resp, nil
}

// Transactions возвращает транзакции счёта за [from, to). Пагинации у метода нет,
// только limit, поэтому вызывающий дробит период на короткие окна.
func (c *Client) Transactions(
	ctx context.Context, accountID string, from, to time.Time, limit int,
) ([]Transaction, error) {
	q := url.Values{}
	q.Set("limit", fmt.Sprint(limit))
	q.Set("interval.start_time", from.UTC().Format(time.RFC3339))
	q.Set("interval.end_time", to.UTC().Format(time.RFC3339))

	var resp struct {
		Transactions []Transaction `json:"transactions"`
	}
	path := "/v1/accounts/" + url.PathEscape(accountID) + "/transactions"
	if err := c.get(ctx, path, q, &resp); err != nil {
		return nil, fmt.Errorf("transactions %s: %w", accountID, err)
	}
	return resp.Transactions, nil
}

// GetAsset возвращает справку по инструменту (название, тип) для счёта.
func (c *Client) GetAsset(ctx context.Context, symbol, accountID string) (*Asset, error) {
	q := url.Values{}
	q.Set("account_id", accountID)
	var resp Asset
	if err := c.get(ctx, "/v1/assets/"+url.PathEscape(symbol), q, &resp); err != nil {
		return nil, fmt.Errorf("get asset %s: %w", symbol, err)
	}
	return &resp, nil
}

// get выполняет авторизованный GET с повтором на временных ошибках. Отвергнутый
// JWT перевыпускается один раз.
func (c *Client) get(ctx context.Context, path string, q url.Values, resp any) error {
	u := c.baseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	reissued := false
	return c.retry(ctx, func() (bool, error) {
		jwt, err := c.token(ctx)
		if err != nil {
			return false, err
		}
		retryable, err := c.do(ctx, http.MethodGet, u, nil, jwt, resp)
		if errors.Is(err, errUnauthorized) && !reissued {
			reissued = true
			c.dropToken()
			return true, err
		}
		return retryable, err
	})
}

// post выполняет POST без авторизации (им пользуется только AuthService).
func (c *Client) post(ctx context.Context, path string, req, resp any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	return c.retry(ctx, func() (bool, error) {
		return c.do(ctx, http.MethodPost, c.baseURL+path, body, "", resp)
	})
}

// retry повторяет вызов до трёх раз с растущей паузой, пока он просит повтора.
func (c *Client) retry(ctx context.Context, call func() (bool, error)) error {
	const attempts = 3
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			delay := time.Duration(1<<uint(i-1)) * 2 * time.Second
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
		retryable, err := call()
		if err == nil {
			return nil
		}
		lastErr = err
		if !retryable {
			return err
		}
	}
	return fmt.Errorf("failed after %d attempts: %w", attempts, lastErr)
}

// do возвращает (retryable, error). jwt пустой — запрос без Authorization.
func (c *Client) do(ctx context.Context, method, u string, body []byte, jwt string, resp any) (bool, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return false, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if jwt != "" {
		// Финам ждёт JWT как есть, без префикса Bearer.
		req.Header.Set("Authorization", jwt)
	}

	httpResp, err := c.http.Do(req)
	if err != nil {
		return true, fmt.Errorf("request %s: %w", redactPath(u), err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(httpResp.Body, 8<<20))
	if err != nil {
		return true, fmt.Errorf("read response %s: %w", redactPath(u), err)
	}

	switch {
	case httpResp.StatusCode == http.StatusUnauthorized:
		return false, fmt.Errorf("%s: HTTP 401: %s: %w", redactPath(u), apiError(raw), errUnauthorized)
	case httpResp.StatusCode != http.StatusOK:
		retryable := httpResp.StatusCode == http.StatusTooManyRequests || httpResp.StatusCode >= 500
		return retryable, fmt.Errorf("%s: HTTP %d: %s", redactPath(u), httpResp.StatusCode, apiError(raw))
	}

	if err := json.Unmarshal(raw, resp); err != nil {
		return false, fmt.Errorf("unmarshal response %s: %w", redactPath(u), err)
	}
	return false, nil
}

// redactPath оставляет от URL только путь — в ошибки и логи не попадают хост и
// параметры запроса.
func redactPath(u string) string {
	if p, err := url.Parse(u); err == nil {
		return p.Path
	}
	return u
}

// apiError вытаскивает описание из тела ошибки (google.rpc.Status).
func apiError(raw []byte) string {
	var e struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &e); err == nil && e.Message != "" {
		return e.Message
	}
	s := strings.TrimSpace(string(raw))
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}
