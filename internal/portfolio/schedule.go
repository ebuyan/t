package portfolio

import (
	"fmt"
	"strings"
	"time"
)

// Schedule — момент запуска: время суток плюс, необязательно, дни недели.
type Schedule struct {
	At   time.Duration  // смещение от полуночи
	Days []time.Weekday // пусто — каждый день
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

// ParseSchedule разбирает «11:00» (каждый день) или «Mon,Fri 11:00».
func ParseSchedule(s string) (Schedule, error) {
	fields := strings.Fields(strings.TrimSpace(s))
	if len(fields) == 0 {
		return Schedule{}, fmt.Errorf("empty schedule")
	}

	clock := fields[len(fields)-1]
	t, err := time.Parse("15:04", clock)
	if err != nil {
		return Schedule{}, fmt.Errorf("expected time HH:MM, got %q", clock)
	}
	sch := Schedule{At: time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute}

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
			return Schedule{}, fmt.Errorf("unknown weekday %q (expected Mon..Sun)", name)
		}
		sch.Days = append(sch.Days, day)
	}
	return sch, nil
}

// Next возвращает ближайшее срабатывание после now в локальной зоне.
func (s Schedule) Next(now time.Time) time.Time {
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	// Максимум неделя вперёд: за 8 итераций подходящий день найдётся всегда.
	for i := 0; i < 8; i++ {
		candidate := midnight.AddDate(0, 0, i).Add(s.At)
		if candidate.After(now) && s.matches(candidate.Weekday()) {
			return candidate
		}
	}
	return midnight.AddDate(0, 0, 7).Add(s.At)
}

func (s Schedule) matches(day time.Weekday) bool {
	if len(s.Days) == 0 {
		return true
	}
	for _, d := range s.Days {
		if d == day {
			return true
		}
	}
	return false
}

// Quarterly — то же время, но только первого числа квартала.
type Quarterly struct {
	At time.Duration
}

func (q Quarterly) Next(now time.Time) time.Time {
	month := time.Month((int(now.Month())-1)/3*3 + 1)
	start := time.Date(now.Year(), month, 1, 0, 0, 0, 0, now.Location()).Add(q.At)
	if start.After(now) {
		return start
	}
	return time.Date(now.Year(), month+3, 1, 0, 0, 0, 0, now.Location()).Add(q.At)
}
