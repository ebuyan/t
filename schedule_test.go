package main

import (
	"testing"
	"time"
)

func TestParseSchedule(t *testing.T) {
	sch, err := parseSchedule("Mon,Fri 11:00")
	if err != nil {
		t.Fatalf("parseSchedule: %v", err)
	}
	if want := 11 * time.Hour; sch.at != want {
		t.Errorf("время = %v, хотим %v", sch.at, want)
	}
	if len(sch.days) != 2 || sch.days[0] != time.Monday || sch.days[1] != time.Friday {
		t.Errorf("дни = %v, хотим [Monday Friday]", sch.days)
	}

	// Без дней — ежедневно.
	daily, err := parseSchedule("19:10")
	if err != nil {
		t.Fatalf("parseSchedule: %v", err)
	}
	if len(daily.days) != 0 {
		t.Errorf("дни = %v, хотим пусто", daily.days)
	}

	for _, bad := range []string{"25:00", "Monday,Fri 11:00", "", "11:00 Mon"} {
		if _, err := parseSchedule(bad); err == nil {
			t.Errorf("parseSchedule(%q) должен вернуть ошибку", bad)
		}
	}
}

func TestScheduleNextWeekdays(t *testing.T) {
	sch, err := parseSchedule("Mon,Fri 11:00")
	if err != nil {
		t.Fatal(err)
	}

	// 2026-07-20 — понедельник.
	monMorning := time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC)
	if got, want := sch.next(monMorning), time.Date(2026, 7, 20, 11, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("утро понедельника → %v, хотим %v", got, want)
	}

	// После срабатывания в понедельник следующий — пятница.
	monAfter := time.Date(2026, 7, 20, 11, 30, 0, 0, time.UTC)
	if got, want := sch.next(monAfter), time.Date(2026, 7, 24, 11, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("после понедельника → %v, хотим пятницу %v", got, want)
	}

	// Из субботы — следующий понедельник.
	sat := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	if got, want := sch.next(sat), time.Date(2026, 7, 27, 11, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("суббота → %v, хотим понедельник %v", got, want)
	}

	// Ровно в момент срабатывания — переносим на следующий раз, чтобы не
	// сработать дважды подряд.
	exact := time.Date(2026, 7, 20, 11, 0, 0, 0, time.UTC)
	if got, want := sch.next(exact), time.Date(2026, 7, 24, 11, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("ровно в 11:00 → %v, хотим %v", got, want)
	}
}

func TestQuarterlyNext(t *testing.T) {
	q := quarterly{at: 10 * time.Hour}

	// Середина квартала — ближайшее первое число следующего.
	if got, want := q.next(time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)),
		time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("июль → %v, хотим %v", got, want)
	}

	// Утро первого числа квартала — сегодня.
	if got, want := q.next(time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)),
		time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("утро 1 октября → %v, хотим %v", got, want)
	}

	// Четвёртый квартал должен перекатываться в январь следующего года.
	if got, want := q.next(time.Date(2026, 12, 20, 12, 0, 0, 0, time.UTC)),
		time.Date(2027, 1, 1, 10, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("декабрь → %v, хотим %v", got, want)
	}
}
