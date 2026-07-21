package portfolio

import "testing"

func TestCacheEmpty(t *testing.T) {
	c := NewCache(nil)
	if _, _, err := c.Snapshot(); err == nil {
		t.Error("ожидалась ошибка на несобранном срезе")
	}
	if _, _, err := c.Meta(); err == nil {
		t.Error("ожидалась ошибка на несобранных метаданных")
	}
}
