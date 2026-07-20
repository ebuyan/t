package main

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

// Dec — число в формате T-Invest API: units (целая часть) + nano (миллиардные доли).
// Реальное значение = units + nano/1e9. Обе части могут быть отрицательными.
// Внутри храним всё в нанодолях, чтобы не терять точность на float.
type Dec struct {
	nanos int64
}

const nanoScale = 1_000_000_000

// jsonNum принимает и число, и строку: REST-обёртка отдаёт int64 строкой.
type jsonNum int64

func (n *jsonNum) UnmarshalJSON(b []byte) error {
	s := string(b)
	if s == "null" {
		*n = 0
		return nil
	}
	if len(s) >= 2 && s[0] == '"' {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		s = str
	}
	if s == "" {
		*n = 0
		return nil
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("не число: %q", s)
	}
	*n = jsonNum(v)
	return nil
}

// Quotation — относительные величины (проценты, количество).
type Quotation struct {
	Units jsonNum `json:"units"`
	Nano  int32   `json:"nano"`
}

func (q Quotation) Dec() Dec {
	return Dec{nanos: int64(q.Units)*nanoScale + int64(q.Nano)}
}

// MoneyValue — денежная величина с валютой.
type MoneyValue struct {
	Currency string  `json:"currency"`
	Units    jsonNum `json:"units"`
	Nano     int32   `json:"nano"`
}

func (m MoneyValue) Dec() Dec {
	return Dec{nanos: int64(m.Units)*nanoScale + int64(m.Nano)}
}

// String печатает значение с prec знаками после запятой, округляя половину от нуля.
func (d Dec) String(prec int) string {
	div := int64(1)
	for i := 0; i < 9-prec; i++ {
		div *= 10
	}

	n := d.nanos
	neg := n < 0
	if neg {
		n = -n
	}
	// Округление половины вверх (по модулю).
	n = (n + div/2) / div

	whole := n / pow10(prec)
	frac := n % pow10(prec)

	sign := ""
	if neg && (whole != 0 || frac != 0) {
		sign = "-"
	}
	if prec == 0 {
		return fmt.Sprintf("%s%d", sign, whole)
	}
	return fmt.Sprintf("%s%d.%0*d", sign, whole, prec, frac)
}

// Add возвращает сумму.
func (d Dec) Add(o Dec) Dec { return Dec{nanos: d.nanos + o.nanos} }

// IsZero — точное сравнение с нулём.
func (d Dec) IsZero() bool { return d.nanos == 0 }

// Mul умножает два значения. Через big.Int: произведение нанодолей выходит
// за int64 уже на суммах порядка миллиона рублей.
func (d Dec) Mul(o Dec) Dec {
	p := new(big.Int).Mul(big.NewInt(d.nanos), big.NewInt(o.nanos))
	p.Quo(p, big.NewInt(nanoScale))
	return Dec{nanos: p.Int64()}
}

// CeilTo округляет вверх до кратного step (step в целых единицах, не нанодолях).
// Математически вверх, то есть к нулю для отрицательных: при step=1000
// -1162021 → -1162000, а 1162021 → 1163000.
func (d Dec) CeilTo(step int64) Dec {
	s := step * nanoScale
	q, r := d.nanos/s, d.nanos%s
	if r > 0 {
		q++
	}
	return Dec{nanos: q * s}
}

// Percent возвращает d/whole*100. Ноль в знаменателе даёт ноль: в таблице это
// строка вида «0%», что честнее, чем падение на пустом счёте.
func (d Dec) Percent(whole Dec) Dec {
	if whole.nanos == 0 {
		return Dec{}
	}
	n := new(big.Int).Mul(big.NewInt(d.nanos), big.NewInt(100*nanoScale))
	n.Quo(n, big.NewInt(whole.nanos))
	return Dec{nanos: n.Int64()}
}

// group расставляет пробелы в целой части числа: -1234567.89 → -1 234 567.89
func group(s string) string {
	sign := ""
	if s != "" && (s[0] == '-' || s[0] == '+') {
		sign, s = string(s[0]), s[1:]
	}
	intPart, frac := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart, frac = s[:i], s[i:]
	}

	var out []byte
	for i, c := range []byte(intPart) {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			out = append(out, ' ')
		}
		out = append(out, c)
	}
	return sign + string(out) + frac
}

func pow10(n int) int64 {
	r := int64(1)
	for i := 0; i < n; i++ {
		r *= 10
	}
	return r
}
