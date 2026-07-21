package portfolio

import (
	"testing"
	"time"
)

func TestParseSchedule(t *testing.T) {
	sch, err := ParseSchedule("Mon,Fri 11:00")
	if err != nil {
		t.Fatalf("ParseSchedule: %v", err)
	}
	if want := 11 * time.Hour; sch.At != want {
		t.Errorf("время = %v, хотим %v", sch.At, want)
	}
	if len(sch.Days) != 2 || sch.Days[0] != time.Monday || sch.Days[1] != time.Friday {
		t.Errorf("дни = %v, хотим [Monday Friday]", sch.Days)
	}

	// Без дней — ежедневно.
	daily, err := ParseSchedule("19:10")
	if err != nil {
		t.Fatalf("ParseSchedule: %v", err)
	}
	if len(daily.Days) != 0 {
		t.Errorf("дни = %v, хотим пусто", daily.Days)
	}

	for _, bad := range []string{"25:00", "Monday,Fri 11:00", "", "11:00 Mon"} {
		if _, err := ParseSchedule(bad); err == nil {
			t.Errorf("ParseSchedule(%q) должен вернуть ошибку", bad)
		}
	}
}

func TestScheduleNextWeekdays(t *testing.T) {
	sch, err := ParseSchedule("Mon,Fri 11:00")
	if err != nil {
		t.Fatal(err)
	}

	// 2026-07-20 — понедельник.
	monMorning := time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC)
	if got, want := sch.Next(monMorning), time.Date(2026, 7, 20, 11, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("утро понедельника → %v, хотим %v", got, want)
	}

	// После срабатывания в понедельник следующий — пятница.
	monAfter := time.Date(2026, 7, 20, 11, 30, 0, 0, time.UTC)
	if got, want := sch.Next(monAfter), time.Date(2026, 7, 24, 11, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("после понедельника → %v, хотим пятницу %v", got, want)
	}

	// Из субботы — следующий понедельник.
	sat := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	if got, want := sch.Next(sat), time.Date(2026, 7, 27, 11, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("суббота → %v, хотим понедельник %v", got, want)
	}

	// Ровно в момент срабатывания — переносим на следующий раз, чтобы не
	// сработать дважды подряд.
	exact := time.Date(2026, 7, 20, 11, 0, 0, 0, time.UTC)
	if got, want := sch.Next(exact), time.Date(2026, 7, 24, 11, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("ровно в 11:00 → %v, хотим %v", got, want)
	}
}

func TestQuarterlyNext(t *testing.T) {
	q := Quarterly{At: 10 * time.Hour}

	// Середина квартала — ближайшее первое число следующего.
	if got, want := q.Next(time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)),
		time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("июль → %v, хотим %v", got, want)
	}

	// Утро первого числа квартала — сегодня.
	if got, want := q.Next(time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)),
		time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("утро 1 октября → %v, хотим %v", got, want)
	}

	// Четвёртый квартал должен перекатываться в январь следующего года.
	if got, want := q.Next(time.Date(2026, 12, 20, 12, 0, 0, 0, time.UTC)),
		time.Date(2027, 1, 1, 10, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("декабрь → %v, хотим %v", got, want)
	}
}
