package main

import (
	"fmt"
	"strings"
	"time"
)

// schedule — момент запуска: время суток плюс, необязательно, дни недели.
type schedule struct {
	at   time.Duration  // смещение от полуночи
	days []time.Weekday // пусто — каждый день
}

var weekdayNames = map[string]time.Weekday{
	"mon": time.Monday,
	"tue": time.Tuesday,
	"wed": time.Wednesday,
	"thu": time.Thursday,
	"fri": time.Friday,
	"sat": time.Saturday,
	"sun": time.Sunday,
}

// parseSchedule разбирает «11:00» (каждый день) или «Mon,Fri 11:00».
func parseSchedule(s string) (schedule, error) {
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) == 0 {
		return schedule{}, fmt.Errorf("пустое расписание")
	}

	clock := fields[len(fields)-1]
	t, err := time.Parse("15:04", clock)
	if err != nil {
		return schedule{}, fmt.Errorf("ожидается время HH:MM, получено %q", clock)
	}
	sch := schedule{at: time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute}

	if len(fields) == 1 {
		return sch, nil
	}
	for _, name := range strings.Split(strings.Join(fields[:len(fields)-1], ","), ",") {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		day, ok := weekdayNames[name]
		if !ok {
			return schedule{}, fmt.Errorf("неизвестный день недели %q (ожидается Mon..Sun)", name)
		}
		sch.days = append(sch.days, day)
	}
	return sch, nil
}

// next возвращает ближайшее срабатывание после now в локальной зоне.
func (s schedule) next(now time.Time) time.Time {
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	// Максимум неделя вперёд: за 8 итераций подходящий день найдётся всегда.
	for i := 0; i < 8; i++ {
		candidate := midnight.AddDate(0, 0, i).Add(s.at)
		if candidate.After(now) && s.matches(candidate.Weekday()) {
			return candidate
		}
	}
	return midnight.AddDate(0, 0, 7).Add(s.at)
}

func (s schedule) matches(day time.Weekday) bool {
	if len(s.days) == 0 {
		return true
	}
	for _, d := range s.days {
		if d == day {
			return true
		}
	}
	return false
}

// quarterly — то же время, но только первого числа квартала.
type quarterly struct {
	at time.Duration
}

func (q quarterly) next(now time.Time) time.Time {
	month := time.Month((int(now.Month())-1)/3*3 + 1)
	start := time.Date(now.Year(), month, 1, 0, 0, 0, 0, now.Location()).Add(q.at)
	if start.After(now) {
		return start
	}
	return time.Date(now.Year(), month+3, 1, 0, 0, 0, 0, now.Location()).Add(q.at)
}
