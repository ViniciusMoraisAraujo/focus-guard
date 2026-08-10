package schedule

import (
	"testing"
)

// Fuzz targets da Etapa 8 do bug-hunt (docs/bug-hunt-plan.md): os parsers de
// entrada não-confiável do pacote schedule — o .ics (RFC 5545, bytes
// arbitrários vindos de upload) e as janelas/relógios "HH:MM" (persistidas em
// schedules.json e enviadas via IPC/CLI/UI). O objetivo é "sem crash"; as
// property checks adicionais garantem que o que PASSOU pelo parser respeita
// o invariante do domínio (minutos 0..1439, janela nunca vazia).
//
// Rode com:
//
//	go test -fuzz=FuzzParseICS -fuzztime=30s ./internal/domain/schedule
//	go test -fuzz=FuzzWindowsPairs -fuzztime=30s ./internal/domain/schedule
//	go test -fuzz=FuzzParseClock -fuzztime=30s ./internal/domain/schedule

// FuzzParseICS alimenta o parser de calendário com bytes arbitrários. O
// contrato do ParseICS é "nunca dar erro duro" (linhas malformadas são
// ignoradas) — o fuzz garante que isso vale para QUALQUER entrada e que toda
// regra devolvida é internamente consistente (dias 0..6 e janela válida).
func FuzzParseICS(f *testing.F) {
	seeds := []string{
		"",
		"BEGIN:VEVENT\nSUMMARY:Estudo\nDTSTART:20260202T080000\nDTEND:20260202T120000\nRRULE:FREQ=WEEKLY;BYDAY=MO,WE,FR\nEND:VEVENT",
		"BEGIN:VEVENT\nRRULE:FREQ=WEEKLY;BYDAY=SU\nDTSTART:20260202T220000\nDTEND:20260203T020000\nEND:VEVENT",
		"BEGIN:VCALENDAR\nBEGIN:VEVENT\nDTSTART:20260202T000000\nDTEND:20260202T235900\nRRULE:FREQ=WEEKLY;BYDAY=TU,TH\nEND:VEVENT\nEND:VCALENDAR",
		"linha sem sentido\nBEGIN:VEVENT\nRRULE:FREQ=MONTHLY\nEND:VEVENT",
		"BEGIN:VEVENT\nDTSTART:20260202\nEND:VEVENT",
		// DTSTART 23:59 sem DTEND: o fallback de 1h cruza a meia-noite —
		// regressão do fix do icsWindow (sem wrap viraria "24:59", rejeitada
		// pelo parseClock).
		"BEGIN:VEVENT\nDTSTART:20260202T235900\nRRULE:FREQ=WEEKLY;BYDAY=MO\nEND:VEVENT",
	}
	for _, s := range seeds {
		f.Add([]byte(s))
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		rules, err := ParseICS(data)
		if err != nil {
			// Contrato: o parser nunca falha duro (não há malformação que
			// faça o parse abortar — linhas desconhecidas são puladas).
			t.Fatalf("ParseICS deu erro para entrada de %d bytes: %v", len(data), err)
		}
		for _, r := range rules {
			// Dias fora de 0..6 quebrariam o windowsPairs/hasDay lá na frente.
			for _, d := range r.Days {
				if d < 0 || d > 6 {
					t.Errorf("regra do ICS com dia inválido %d (0-6)", d)
				}
			}
			// A janela "HH:MM-HH:MM" gerada pelo icsWindow deve ser válida.
			if _, err := windowsPairs(r); err != nil {
				t.Errorf("janela do ICS inválida %q: %v", r.Windows, err)
			}
		}
	})
}

// FuzzWindowsPairs exercita o parser de janelas "HH:MM-HH:MM" (o formato
// persistido e enviado pela UI/CLI). Property: quando o parse passa, os
// extremos estão em 0..1439 e a janela nunca é vazia (start != end — o
// invariante que evita um bloqueio sempre-ativo).
func FuzzWindowsPairs(f *testing.F) {
	seeds := []string{
		"08:00-12:00",
		"22:00-02:00", // overnight
		"00:00-23:59",
		"08:00-08:00",    // inválida (vazia)
		"24:00-01:00",    // inválida (hora 24)
		"8:00-9:00",      // inválida (sem zero-pad; o parser exige HH:MM)
		"08:00",          // inválida (sem fim)
		"08:00-12:00-14", // inválida (3 partes)
		"",               // inválida (vazia)
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, w string) {
		pairs, err := windowsPairs(Rule{Windows: []string{w}})
		if err != nil {
			return // parse rejeitou — comportamento válido
		}
		if len(pairs) != 1 {
			t.Fatalf("esperava 1 par para %q, got %d", w, len(pairs))
		}
		p := pairs[0]
		if p[0] < 0 || p[0] >= 1440 || p[1] < 0 || p[1] >= 1440 {
			t.Errorf("janela %q → par fora do dia %v", w, p)
		}
		if p[0] == p[1] {
			t.Errorf("janela %q → par vazio %v (start == end)", w, p)
		}
	})
}

// FuzzParseClock exercita o parser de relógio "HH:MM" individual. Property:
// sucesso implica minutos 0..1439 (o parser já valida h 0..23 e m 0..59; o
// fuzz congela o invariante e cobre o caso "24:00"/"00:60" e afins).
func FuzzParseClock(f *testing.F) {
	seeds := []string{
		"00:00", "12:34", "23:59",
		"24:00", "00:60", "-1:00", "08", "8:00", "1:2", "", " 08:00 ",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		got, err := parseClock(s)
		if err != nil {
			return // rejeitado — ok
		}
		if got < 0 || got >= 1440 {
			t.Errorf("parseClock(%q) = %d fora de 0..1439", s, got)
		}
	})
}
