package schedule

import (
	"path/filepath"
	"testing"
)

const sampleICS = `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//FocusGuard Test//PT
BEGIN:VEVENT
UID:1
SUMMARY:Estudo matinal
DTSTART:20260202T080000
DTEND:20260202T120000
RRULE:FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR
END:VEVENT
BEGIN:VEVENT
UID:2
SUMMARY:Revisao sabado
DTSTART:20260207T140000
DTEND:20260207T180000
RRULE:FREQ=WEEKLY;BYDAY=SA
END:VEVENT
END:VCALENDAR
`

// TestParseICS_WeeklyEvents verifies a weekly-recurrence .ics maps onto the
// existing recurring Rule model (days + windows + label).
func TestParseICS_WeeklyEvents(t *testing.T) {
	rules, err := ParseICS([]byte(sampleICS))
	if err != nil {
		t.Fatalf("ParseICS: %v", err)
	}
	if len(rules) != 2 {
		t.Fatalf("len(rules) = %d, want 2", len(rules))
	}

	r0 := rules[0]
	if r0.Label != "Estudo matinal" {
		t.Errorf("Label = %q, want Estudo matinal", r0.Label)
	}
	if len(r0.Days) != 5 || r0.Days[0] != 1 || r0.Days[4] != 5 {
		t.Errorf("Days = %v, want [1 2 3 4 5] (seg-sex)", r0.Days)
	}
	if len(r0.Windows) != 1 || r0.Windows[0] != "08:00-12:00" {
		t.Errorf("Windows = %v, want [08:00-12:00]", r0.Windows)
	}

	r1 := rules[1]
	if r1.Label != "Revisao sabado" || len(r1.Days) != 1 || r1.Days[0] != 6 {
		t.Errorf("unexpected second rule: %+v", r1)
	}
}

// TestParseICS_SkipsNonWeekly verifies events without a weekly recurrence are
// skipped (the product only models weekly recurring blocks).
func TestParseICS_SkipsNonWeekly(t *testing.T) {
	ics := `BEGIN:VCALENDAR
BEGIN:VEVENT
UID:1
SUMMARY:Once
DTSTART:20260202T080000
DTEND:20260202T120000
END:VEVENT
BEGIN:VEVENT
UID:2
SUMMARY:Mensal
DTSTART:20260202T080000
DTEND:20260202T120000
RRULE:FREQ=MONTHLY;BYDAY=1MO
END:VEVENT
BEGIN:VEVENT
UID:3
SUMMARY:Semanal
DTSTART:20260202T080000
DTEND:20260202T120000
RRULE:FREQ=WEEKLY;BYDAY=WE
END:VEVENT
END:VCALENDAR
`
	rules, err := ParseICS([]byte(ics))
	if err != nil {
		t.Fatalf("ParseICS: %v", err)
	}
	if len(rules) != 1 || rules[0].Label != "Semanal" {
		t.Fatalf("rules = %+v, want only the weekly event", rules)
	}
}

// TestParseICS_Garbage verifies malformed input never crashes.
func TestParseICS_Garbage(t *testing.T) {
	if _, err := ParseICS([]byte("{nao é ics")); err != nil {
		t.Fatalf("garbage should parse to zero rules, got error: %v", err)
	}
}

// TestManager_ImportFromICS verifies ImportICS parses, validates and persists
// the rules through the Manager (exercising the real Add path).
func TestManager_ImportFromICS(t *testing.T) {
	m := NewManager(filepath.Join(t.TempDir(), "schedules.json"))

	added, err := m.ImportICS([]byte(sampleICS), "social")
	if err != nil {
		t.Fatalf("ImportICS: %v", err)
	}
	if len(added) != 2 {
		t.Fatalf("added = %d, want 2", len(added))
	}
	for _, r := range added {
		if r.Preset != "social" {
			t.Errorf("Preset = %q, want social", r.Preset)
		}
		if r.ID == "" || !r.Enabled {
			t.Errorf("rule should have an ID and be enabled: %+v", r)
		}
	}

	// Persistiu de verdade: um novo Manager no mesmo path vê as regras.
	m2 := NewManager(m.path)
	if len(m2.List()) != 2 {
		t.Errorf("reloaded rules = %d, want 2", len(m2.List()))
	}
}
