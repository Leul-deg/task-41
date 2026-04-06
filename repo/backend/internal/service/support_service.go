package service

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"meridian/backend/internal/domain/support"
)

type SupportService struct {
	DB *sql.DB
}

func NewSupportService(db *sql.DB) *SupportService {
	return &SupportService{DB: db}
}

func (s *SupportService) EvaluateEligibility(orderID string, requestedType string) (support.EligibilityResult, error) {
	var deliveredAt time.Time
	err := s.DB.QueryRow(`
		SELECT canonical_delivered_at
		FROM delivery_events
		WHERE order_id=$1 AND canonical_delivered_at IS NOT NULL
		ORDER BY event_time DESC
		LIMIT 1
	`, orderID).Scan(&deliveredAt)
	if err != nil {
		return support.EligibilityResult{}, errors.New("canonical delivery event not found")
	}

	deadline := deliveredAt.Add(30 * 24 * time.Hour)
	if time.Now().After(deadline) {
		return support.EligibilityResult{
			ReturnAllowed: false,
			RefundOnly:    true,
			Reason:        "outside return window",
			Deadline:      deadline,
		}, nil
	}

	rows, err := s.DB.Query(`SELECT returnable FROM order_lines WHERE order_id=$1`, orderID)
	if err != nil {
		return support.EligibilityResult{}, err
	}
	defer rows.Close()

	hasNonReturnable := false
	hasAny := false
	for rows.Next() {
		hasAny = true
		var ret bool
		if err := rows.Scan(&ret); err == nil && !ret {
			hasNonReturnable = true
		}
	}
	if !hasAny {
		return support.EligibilityResult{}, errors.New("order has no lines")
	}

	requestedType = strings.ToLower(strings.TrimSpace(requestedType))
	if hasNonReturnable && requestedType == "return_and_refund" {
		return support.EligibilityResult{
			ReturnAllowed: true,
			RefundOnly:    true,
			Reason:        "mixed order contains non-returnable items; enforce line-level refund-only",
			Deadline:      deadline,
		}, nil
	}

	return support.EligibilityResult{
		ReturnAllowed: true,
		RefundOnly:    requestedType == "refund_only",
		Reason:        "eligible",
		Deadline:      deadline,
	}, nil
}

func (s *SupportService) ComputeSLADue(siteCode, priority string, from time.Time) (time.Time, error) {
	var tz string
	var start, end string
	var weekendCSV, holidaysCSV string
	err := s.DB.QueryRow(`
		SELECT timezone, business_start::text, business_end::text,
		COALESCE(array_to_string(weekend_days, ','), ''),
		COALESCE(array_to_string(holidays, ','), '')
		FROM business_calendars WHERE site_code=$1
	`, siteCode).Scan(&tz, &start, &end, &weekendCSV, &holidaysCSV)
	if err != nil {
		return time.Time{}, err
	}

	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}

	needHours := businessHoursForPriority(priority, start, end)
	if strings.EqualFold(priority, "HIGH") {
		needHours = 4.0
	}

	cur := from.In(loc)
	weekends := parseWeekendDays(weekendCSV)
	holidays := parseHolidays(holidaysCSV)
	minutesNeeded := int(needHours * 60)
	for minutesNeeded > 0 {
		if isBusinessMinute(cur, start, end, weekends, holidays) {
			minutesNeeded--
		}
		cur = cur.Add(1 * time.Minute)
	}
	return cur.UTC(), nil
}

func isBusinessMinute(t time.Time, start string, end string, weekendDays map[int]bool, holidays map[string]bool) bool {
	if weekendDays[int(t.Weekday())] {
		return false
	}
	if holidays[t.Format("2006-01-02")] {
		return false
	}
	hhmm := fmt.Sprintf("%02d:%02d", t.Hour(), t.Minute())
	return hhmm >= start[:5] && hhmm < end[:5]
}

func parseWeekendDays(csv string) map[int]bool {
	out := map[int]bool{}
	for _, p := range strings.Split(strings.TrimSpace(csv), ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err == nil {
			out[n] = true
		}
	}
	if len(out) == 0 {
		out[0] = true
		out[6] = true
	}
	return out
}

func parseHolidays(csv string) map[string]bool {
	out := map[string]bool{}
	for _, p := range strings.Split(strings.TrimSpace(csv), ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			out[p] = true
		}
	}
	return out
}

func businessHoursForPriority(priority, start, end string) float64 {
	if strings.EqualFold(priority, "HIGH") {
		return 4.0
	}
	startMinutes, okStart := parseClockMinutes(start)
	endMinutes, okEnd := parseClockMinutes(end)
	if !okStart || !okEnd || endMinutes <= startMinutes {
		return 8.0
	}
	return float64(endMinutes-startMinutes) / 60.0
}

func parseClockMinutes(value string) (int, bool) {
	if len(value) < 5 {
		return 0, false
	}
	hours, err := strconv.Atoi(value[:2])
	if err != nil {
		return 0, false
	}
	minutes, err := strconv.Atoi(value[3:5])
	if err != nil {
		return 0, false
	}
	return hours*60 + minutes, true
}
