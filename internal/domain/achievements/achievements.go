// Package achievements implements the focus gamification badges (Fase 5.2 do
// features-plan): a pure catalog that derives unlocked badges from the
// analytics Stats report — no persistence of "already earned", no state
// migration. Every badge is recomputed on read from the same stats the
// dashboard shows, so the calculations can never drift from the data.
package achievements

import (
	"time"

	"focusguard/internal/domain/analytics"
)

// Achievement is one badge. Unlocked is derived from the stats; Progress is
// 0-100 toward unlocking (100 = unlocked).
type Achievement struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Icon        string `json:"icon,omitempty"`
	Unlocked    bool   `json:"unlocked"`
	Progress    int    `json:"progress"`
}

// percent arredonda a razão num/den para 0-100 (den <= 0 → 0).
func percent(num, den int) int {
	if den <= 0 {
		return 0
	}
	p := num * 100 / den
	if p > 100 {
		p = 100
	}
	if p < 0 {
		p = 0
	}
	return p
}

// Calculate derives the badges from the stats report (pure — same input,
// same output; the UI shows them all with reduced opacity for locked ones).
// now é a referência temporal das janelas ("últimos 30 dias" do Mês de
// Foco) — passada pelo chamador para o cálculo ficar determinístico.
func Calculate(st *analytics.Stats, sessions []analytics.Session, now time.Time) []Achievement {
	if st == nil {
		return []Achievement{}
	}

	focusHours := int(st.TotalFocus / time.Hour)
	streak := st.Streak
	totalSessions := st.TotalSessions

	// Foco dos últimos 30 dias (janela "um mês" do Mês de Foco): soma o
	// Focus das sessões que TERMINARAM dentro dos últimos 30 dias (fronteira
	// INCLUSIVA: terminar exatamente no marco ainda conta — !Before) — não o
	// total histórico (antes o badge desbloqueava cedo com 40h espalhadas
	// por meses/anos).
	monthCutoff := now.AddDate(0, 0, -30)
	monthFocus := time.Duration(0)
	for _, s := range sessions {
		if !s.End.Before(monthCutoff) {
			monthFocus += s.Focus
		}
	}
	monthMin := int(monthFocus / time.Minute)

	// Sessões após a meia-noite (madrugada): contagem para o "Guardião da
	// Madrugada".
	lateSessions := 0
	for _, s := range sessions {
		h := s.Start.Hour()
		if h >= 0 && h < 5 {
			lateSessions++
		}
	}

	// Missões (sessões nomeadas) distintas.
	missions := make(map[string]bool)
	for _, s := range sessions {
		if s.Label != "" {
			missions[s.Label] = true
		}
	}

	// Dias com foco (dos últimos 7) e dias ativos consecutivos a partir de hoje.
	activeDays := 0
	for _, d := range st.PerDay {
		if d.Duration > 0 {
			activeDays++
		}
	}

	return []Achievement{
		{
			ID:          "first-step",
			Name:        "Primeiro Passo",
			Description: "Complete a primeira sessão de foco.",
			Icon:        "🚀",
			Unlocked:    totalSessions >= 1,
			Progress:    percent(totalSessions, 1),
		},
		{
			ID:          "steel-focus",
			Name:        "Foco de Aço",
			Description: "Acumule 10 horas de foco no total.",
			Icon:        "🛡",
			Unlocked:    focusHours >= 10,
			Progress:    percent(focusHours, 10),
		},
		{
			ID:          "unstoppable",
			Name:        "Imparável",
			Description: "Mantenha uma raia de 7 dias com foco.",
			Icon:        "🔥",
			Unlocked:    streak >= 7,
			Progress:    percent(streak, 7),
		},
		{
			ID:          "night-guardian",
			Name:        "Guardião da Madrugada",
			Description: "Faça 5 sessões de foco entre 0h e 5h.",
			Icon:        "🌙",
			Unlocked:    lateSessions >= 5,
			Progress:    percent(lateSessions, 5),
		},
		{
			ID:          "marathoner",
			Name:        "Maratonista",
			Description: "Alcance 2 horas de foco em um único dia.",
			Icon:        "🏃",
			Unlocked:    anyDayAtLeast(st.PerDay, 2*time.Hour),
			Progress:    percent(dayMaxHours(st.PerDay), 2),
		},
		{
			ID:          "quarter",
			Name:        "Hora de Ouro",
			Description: "Complete 25 sessões de foco.",
			Icon:        "⏱",
			Unlocked:    totalSessions >= 25,
			Progress:    percent(totalSessions, 25),
		},
		{
			ID:          "mission-master",
			Name:        "Mestre das Missões",
			Description: "Registre 3 missões diferentes (sessões nomeadas).",
			Icon:        "🎯",
			Unlocked:    len(missions) >= 3,
			Progress:    percent(len(missions), 3),
		},
		{
			ID:          "centurion",
			Name:        "Centurião",
			Description: "Acumule 100 horas de foco no total.",
			Icon:        "🏆",
			Unlocked:    focusHours >= 100,
			Progress:    percent(focusHours, 100),
		},
		{
			ID:          "week-warrior",
			Name:        "Guerreiro da Semana",
			Description: "Tenha foco em 5 dias diferentes na última semana.",
			Icon:        "📅",
			Unlocked:    activeDays >= 5,
			Progress:    percent(activeDays, 5),
		},
		{
			ID:          "deep-diver",
			Name:        "Mergulhador Profundo",
			Description: "Complete 90 minutos de foco numa única sessão.",
			Icon:        "🤿",
			Unlocked:    anySessionAtLeast(sessions, 90*time.Minute),
			Progress:    percent(maxSessionMinutes(sessions), 90),
		},
		{
			ID:          "fifteen-focus",
			Name:        "Quinze de Foco",
			Description: "Complete 15 horas de foco.",
			Icon:        "⏰",
			Unlocked:    focusHours >= 15,
			Progress:    percent(focusHours, 15),
		},
		{
			ID:          "focused-month",
			Name:        "Mês de Foco",
			Description: "Acumule 40 horas de foco em um mês (últimos 30 dias).",
			Icon:        "📆",
			Unlocked:    monthMin >= 2400,
			Progress:    percent(monthMin, 2400),
		},
	}
}

// anyDayAtLeast reports whether any day in the window reached at least d of
// focus.
func anyDayAtLeast(days []analytics.DayStat, d time.Duration) bool {
	for _, day := range days {
		if day.Duration >= d {
			return true
		}
	}
	return false
}

// dayMaxHours devolve o maior foco diário (em horas inteiras, teto em 2 para o
// progresso do Maratonista).
func dayMaxHours(days []analytics.DayStat) int {
	max := time.Duration(0)
	for _, day := range days {
		if day.Duration > max {
			max = day.Duration
		}
	}
	return int(max / time.Hour)
}

// anySessionAtLeast reports whether any session reached at least d of focus.
func anySessionAtLeast(sessions []analytics.Session, d time.Duration) bool {
	for _, s := range sessions {
		if s.Focus >= d {
			return true
		}
	}
	return false
}

// maxSessionMinutes devolve a maior sessão em minutos (teto natural para o
// progresso do Mergulhador Profundo).
func maxSessionMinutes(sessions []analytics.Session) int {
	max := time.Duration(0)
	for _, s := range sessions {
		if s.Focus > max {
			max = s.Focus
		}
	}
	return int(max / time.Minute)
}
