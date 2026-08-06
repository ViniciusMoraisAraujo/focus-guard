package schedule

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
)

// icsDayNames maps iCal BYDAY values (RFC 5545) to time.Weekday ints (0=Sun).
var icsDayNames = map[string]int{
	"SU": 0, "MO": 1, "TU": 2, "WE": 3, "TH": 4, "FR": 5, "SA": 6,
}

// ParseICS reads an .ics calendar (RFC 5545) and converts every VEVENT with a
// weekly recurrence (RRULE:FREQ=WEEKLY) into a recurring block rule: label =
// SUMMARY, days = BYDAY list, window = DTSTART-DTEND as HH:MM-HH:MM. Events
// without a weekly RRULE (single, monthly, etc.) are skipped — the product
// only models weekly recurring windows. Malformed lines are ignored; the
// parser never returns a hard error for content it cannot understand.
func ParseICS(data []byte) ([]Rule, error) {
	var rules []Rule
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var (
		inEvent bool
		summary string
		start   string
		end     string
		rrule   string
	)

	flush := func() {
		if !inEvent {
			return
		}
		if r, ok := icsEventToRule(summary, start, end, rrule); ok {
			rules = append(rules, r)
		}
		summary, start, end, rrule = "", "", "", ""
	}

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "BEGIN:VEVENT"):
			flush() // VEVENT aninhado (defensivo) — descarta o anterior incompleto
			inEvent = true
		case strings.HasPrefix(line, "END:VEVENT"):
			flush()
			inEvent = false
		case inEvent && strings.HasPrefix(line, "SUMMARY"):
			summary = icsValue(line)
		case inEvent && strings.HasPrefix(line, "DTSTART"):
			start = icsValue(line)
		case inEvent && strings.HasPrefix(line, "DTEND"):
			end = icsValue(line)
		case inEvent && strings.HasPrefix(line, "RRULE"):
			rrule = strings.ToUpper(icsValue(line))
		}
	}
	flush()
	return rules, nil
}

// icsValue returns the value after the first ':' (handles the common
// non-standard "TZID=America/...:20260202T080000" form too).
func icsValue(line string) string {
	idx := strings.Index(line, ":")
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(line[idx+1:])
}

// icsEventToRule converts one VEVENT's fields into a Rule when it has a valid
// weekly recurrence; ok=false otherwise.
func icsEventToRule(summary, start, end, rrule string) (Rule, bool) {
	// Só modelos semanais (FREQ=WEEKLY). RRULE ausente, mensal, etc. → skip.
	if !strings.Contains(rrule, "FREQ=WEEKLY") {
		return Rule{}, false
	}

	days, ok := icsByDay(rrule)
	if !ok || len(days) == 0 {
		return Rule{}, false
	}

	window, ok := icsWindow(start, end)
	if !ok {
		return Rule{}, false
	}

	label := strings.TrimSpace(summary)
	if label == "" {
		label = "Importado do calendário"
	}
	return Rule{
		Label:   label,
		Days:    days,
		Windows: []string{window},
		Enabled: true,
	}, true
}

// icsByDay extracts the BYDAY list from the RRULE, mapping SU..SA to weekday
// ints. Only plain day codes are supported (no ordinal "1MO").
func icsByDay(rrule string) ([]int, bool) {
	byDay := ""
	for _, param := range strings.Split(rrule, ";") {
		if strings.HasPrefix(param, "BYDAY=") {
			byDay = strings.TrimPrefix(param, "BYDAY=")
			break
		}
	}
	if byDay == "" {
		return nil, false
	}

	seen := make(map[int]bool)
	var days []int
	for _, tok := range strings.Split(byDay, ",") {
		tok = strings.TrimSpace(tok)
		// Ordenais como "1MO" não são suportados — pula o dia (e o evento).
		if len(tok) != 2 {
			continue
		}
		d, ok := icsDayNames[tok]
		if !ok {
			continue
		}
		if !seen[d] {
			seen[d] = true
			days = append(days, d)
		}
	}
	if len(days) == 0 {
		return nil, false
	}
	return days, true
}

// icsWindow parses DTSTART/DTEND into an "HH:MM-HH:MM" window. Accepts
// "20260202T080000" (floating local) and the leading-date "20260202" all-day
// form is rejected (no time). When DTEND is missing, a one-hour window is
// assumed from DTSTART.
func icsWindow(start, end string) (string, bool) {
	s, ok := icsClock(start)
	if !ok {
		return "", false
	}
	e, ok := icsClock(end)
	if !ok {
		e = s + 60 // DTEND ausente → 1h
	}
	if s == e {
		return "", false
	}
	return fmt.Sprintf("%s-%s", clockStr(s), clockStr(e)), true
}

// icsClock parses "20260202T080000" into minutes since midnight.
func icsClock(v string) (int, bool) {
	if len(v) != 15 { // YYYYMMDDTHHMMSS
		return 0, false
	}
	h, err1 := strconv.Atoi(v[9:11])
	m, err2 := strconv.Atoi(v[11:13])
	if err1 != nil || err2 != nil || h > 23 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

func clockStr(minutes int) string {
	return fmt.Sprintf("%02d:%02d", minutes/60, minutes%60)
}

// ImportICS parses an .ics file and persists each weekly event as a new rule
// with the given preset, returning the added rules. All rules are validated
// up-front so a mid-way failure cannot leave a partial import persisted.
func (m *Manager) ImportICS(data []byte, preset string) ([]Rule, error) {
	parsed, err := ParseICS(data)
	if err != nil {
		return nil, err
	}

	// Validação antecipada: só adiciona se TODAS as regras passarem — um erro
	// no meio do loop não pode deixar metade do calendário importado.
	for i := range parsed {
		parsed[i].Preset = preset
		if err := validateRule(parsed[i]); err != nil {
			return nil, err
		}
	}

	added := make([]Rule, 0, len(parsed))
	for _, r := range parsed {
		got, err := m.Add(r)
		if err != nil {
			return added, err
		}
		added = append(added, got)
	}
	return added, nil
}
