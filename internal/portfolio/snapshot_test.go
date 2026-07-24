package portfolio

import "testing"

func TestTrimShareSuffix(t *testing.T) {
	cases := map[string]string{
		"Сбербанк - акции привилегированные":  "Сбербанк",
		"Сбербанк - акции привилегированные ": "Сбербанк", // хвостовой пробел
		"Сбербанк":  "Сбербанк", // без суффикса — как есть
		"  Полюс  ": "Полюс",    // только обрезка пробелов
		"":          "",
	}
	for in, want := range cases {
		if got := trimShareSuffix(in); got != want {
			t.Errorf("trimShareSuffix(%q) = %q, ожидалось %q", in, got, want)
		}
	}
}
