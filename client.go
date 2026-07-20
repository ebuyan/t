package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

const baseURL = "https://invest-public-api.tbank.ru/rest/tinkoff.public.invest.api.contract.v1"

// Сертификат *.tbank.ru выпущен НУЦ Минцифры («Russian Trusted Root CA»), которого нет
// в стандартных хранилищах доверия — ни в alpine, ни в macOS/Windows. Поэтому корень
// вшит в бинарник и добавляется только в пул этого клиента: системное хранилище не
// трогаем, доверие не распространяется на остальные приложения хоста.
// Источник: https://gu-st.ru/content/Other/doc/russiantrustedca.pem
// SHA-256: D26D2D0231B7C39F92CC738512BA54103519E4405D68B5BD703E9788CA8ECF31
//
//go:embed russian_trusted_root_ca.pem
var russianTrustedRootCA []byte

type Client struct {
	token string
	http  *http.Client
}

func NewClient(token string) *Client {
	return &Client{
		token: token,
		http: &http.Client{
			Timeout:   30 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: certPool()}},
		},
	}
}

// certPool — системные корни плюс корень НУЦ Минцифры.
func certPool() *x509.CertPool {
	pool, err := x509.SystemCertPool()
	if err != nil {
		log.Printf("системное хранилище сертификатов недоступно (%v), используем только вшитый корень", err)
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(russianTrustedRootCA) {
		log.Print("не удалось разобрать вшитый корневой сертификат НУЦ Минцифры")
	}
	return pool
}

// call выполняет unary-вызов REST-обёртки над gRPC с повтором на временных ошибках.
func (c *Client) call(ctx context.Context, service, method string, req, resp any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("сериализация запроса: %w", err)
	}

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

		retryable, err := c.do(ctx, service, method, body, resp)
		if err == nil {
			return nil
		}
		lastErr = err
		if !retryable {
			return err
		}
	}
	return fmt.Errorf("%s/%s: не удалось за %d попытки: %w", service, method, attempts, lastErr)
}

// do возвращает (retryable, error).
func (c *Client) do(ctx context.Context, service, method string, body []byte, resp any) (bool, error) {
	url := fmt.Sprintf("%s.%s/%s", baseURL, service, method)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.token)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	httpResp, err := c.http.Do(httpReq)
	if err != nil {
		return true, fmt.Errorf("запрос %s: %w", method, err)
	}
	defer httpResp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(httpResp.Body, 8<<20))
	if err != nil {
		return true, fmt.Errorf("чтение ответа %s: %w", method, err)
	}

	if httpResp.StatusCode != http.StatusOK {
		retryable := httpResp.StatusCode == http.StatusTooManyRequests || httpResp.StatusCode >= 500
		return retryable, fmt.Errorf("%s: HTTP %d: %s", method, httpResp.StatusCode, apiError(raw))
	}

	if err := json.Unmarshal(raw, resp); err != nil {
		return false, fmt.Errorf("разбор ответа %s: %w", method, err)
	}
	return false, nil
}

// apiError вытаскивает человекочитаемое описание из тела ошибки API.
func apiError(raw []byte) string {
	var e struct {
		Code        int    `json:"code"`
		Message     string `json:"message"`
		Description string `json:"description"`
	}
	if err := json.Unmarshal(raw, &e); err == nil && e.Message != "" {
		if e.Description != "" {
			return fmt.Sprintf("%s (%s)", e.Message, e.Description)
		}
		return e.Message
	}
	s := string(bytes.TrimSpace(raw))
	if len(s) > 300 {
		s = s[:300] + "…"
	}
	return s
}

type Account struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Status      string `json:"status"`
	AccessLevel string `json:"accessLevel"`
}

func (c *Client) GetAccounts(ctx context.Context) ([]Account, error) {
	var resp struct {
		Accounts []Account `json:"accounts"`
	}
	req := map[string]string{"status": "ACCOUNT_STATUS_OPEN"}
	if err := c.call(ctx, "UsersService", "GetAccounts", req, &resp); err != nil {
		return nil, err
	}
	return resp.Accounts, nil
}

type Portfolio struct {
	AccountID             string     `json:"accountId"`
	TotalAmountPortfolio  MoneyValue `json:"totalAmountPortfolio"`
	TotalAmountShares     MoneyValue `json:"totalAmountShares"`
	TotalAmountBonds      MoneyValue `json:"totalAmountBonds"`
	TotalAmountEtf        MoneyValue `json:"totalAmountEtf"`
	TotalAmountCurrencies MoneyValue `json:"totalAmountCurrencies"`
	TotalAmountFutures    MoneyValue `json:"totalAmountFutures"`
	// ExpectedYield — относительная доходность портфеля в процентах.
	ExpectedYield      Quotation  `json:"expectedYield"`
	DailyYield         MoneyValue `json:"dailyYield"`
	DailyYieldRelative Quotation  `json:"dailyYieldRelative"`
	Positions          []Position `json:"positions"`
}

type Position struct {
	Figi           string     `json:"figi"`
	Ticker         string     `json:"ticker"`
	InstrumentType string     `json:"instrumentType"`
	InstrumentUID  string     `json:"instrumentUid"`
	Quantity       Quotation  `json:"quantity"`
	CurrentPrice   MoneyValue `json:"currentPrice"`
	// ExpectedYield — абсолютная доходность позиции в валюте инструмента.
	ExpectedYield Quotation `json:"expectedYield"`
}

// TotalYield — доходность за всё время в деньгах: сумма доходностей позиций.
// В PortfolioResponse готового поля нет, но сумма сходится с expectedYield:
// сумма/(кол-во × средняя цена) даёт ровно тот процент, что отдаёт API.
//
// Доходность позиции — Quotation без валюты, поэтому валюту берём из currentPrice
// и складываем только позиции в валюте портфеля. Остальные возвращаем отдельно,
// чтобы не показать молча заниженную сумму.
func (p *Portfolio) TotalYield() (total Dec, skipped []string) {
	want := p.TotalAmountPortfolio.Currency
	var sum int64
	for _, pos := range p.Positions {
		if cur := pos.CurrentPrice.Currency; cur != "" && cur != want {
			skipped = append(skipped, positionLabel(pos))
			continue
		}
		sum += pos.ExpectedYield.Dec().nanos
	}
	return Dec{nanos: sum}, skipped
}

func positionLabel(p Position) string {
	if p.Ticker != "" {
		return p.Ticker
	}
	return p.Figi
}

func (c *Client) GetPortfolio(ctx context.Context, accountID, currency string) (*Portfolio, error) {
	var resp Portfolio
	req := map[string]string{"accountId": accountID, "currency": currency}
	if err := c.call(ctx, "OperationsService", "GetPortfolio", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
