// Package clockguard implements the Clock Tamper Protection (Fase 2 do
// features-plan): detecting and neutralizing attempts to fool the wall clock
// into expiring (or extending) focus blocks.
//
// The attack surface is the PERSISTED path: time.Now() reads the OS clock, so
// a user who changes the date can make a block's ExpiresAt look reached
// (advance the clock + restart the daemon) or never reached (rewind). The
// in-process timers are monotonic and immune; the restart boundary is not.
//
// Defense in depth:
//  1. Gap detection: the daemon persists the last trusted wall-clock reading
//     (LastKnownTime). On boot and periodically, |now − lastKnown| beyond a
//     tolerance (with a grace window for legitimate NTP/DST adjustments)
//     raises suspicion. BOTH directions are covered: rewinding to delay
//     expiry and advancing to expire blocks early.
//  2. NTP validation: a public NTP query confirms the real time. The local
//     OS clock can be forged; the NTP server's answer cannot.
//  3. Lockdown: a confirmed jump triggers a preventive all-internet block
//     until NTP validates again, and pending expirations are re-anchored to
//     the corrected time.
package clockguard

import (
	"fmt"
	"log"
	"time"
)

// Tolerance is the maximum |now − lastKnown| accepted without suspicion. 5
// minutes absorbs legitimate NTP/DST adjustments and clock drift between
// restarts, while still catching the classic "set the clock back 2 hours"
// and "advance to tomorrow" tricks.
const Tolerance = 5 * time.Minute

// CheckInterval is how often the daemon re-runs the periodic validation
// (10 minutes). The boot check is always immediate.
const CheckInterval = 10 * time.Minute

// NTPClient is the minimal NTP surface the guard needs (satisfied by
// *ntp.Client). Time returns the server's current wall clock.
type NTPClient interface {
	Time() (time.Time, error)
}

// State is the persisted surface the guard reads/writes (satisfied by
// *scheduler.Scheduler): the last trusted reading and the all-internet
// lockdown (BlockAllInternet is the preventive measure — the same sentinel
// the panic/deep-focus modes use, with its own expiry).
type State interface {
	LastKnownTime() time.Time
	SetLastKnownTime(t time.Time) error
}

// Lockdown interface: when a jump is CONFIRMED by NTP, the guard applies the
// preventive block and records the tamper event. The daemon wires it to the
// scheduler (BlockAllInternet — the same all-internet sentinel the panic
// mode uses, so it has its own expiry and reuses the full expiry machinery).
type Lockdown interface {
	BlockAllInternet(allowlist []string, duration time.Duration) error
}

// Logger records a confirmed tamper (the daemon wires it to the tamper
// recorder — a no-op logger keeps the guard usable in tests).
type Logger interface {
	Log(source, action, detail string)
}

// Guard runs the clock-tamper detection. All dependencies are injectable so
// tests exercise every branch without a real clock or network.
type Guard struct {
	state  State
	ntp    NTPClient
	lock   Lockdown
	now    func() time.Time
	logger Logger
	// lockdownDuration é quanto dura o bloqueio preventivo quando a burla é
	// confirmada (até NTP validar de novo; o guard re-tenta a validação no
	// próximo ciclo).
	lockdownDuration time.Duration
}

// Deps wires a guard. ntp may be nil (offline daemon): a nil NTP client makes
// Check treat any suspicion as UNRESOLVED (keeps the previous verdict) instead
// of confirming or clearing it.
type Deps struct {
	State            State
	NTP              NTPClient
	Lockdown         Lockdown
	Now              func() time.Time
	Logger           Logger
	LockdownDuration time.Duration
}

// New builds a guard with the dependencies. A nil Now falls back to
// time.Now; LockdownDuration defaults to 1 hour.
func New(d Deps) *Guard {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	dur := d.LockdownDuration
	if dur <= 0 {
		dur = time.Hour
	}
	return &Guard{
		state: d.State, ntp: d.NTP, lock: d.Lockdown,
		now: now, logger: d.Logger, lockdownDuration: dur,
	}
}

// Outcome reports what a Check run decided.
type Outcome struct {
	// Suspicion true = the wall clock jumped beyond tolerance.
	Suspicion bool
	// Confirmed true = NTP agreed the local clock is wrong (tamper).
	Confirmed bool
	// Detail is a human-readable summary of the decision.
	Detail string
}

// Check runs one clock validation pass: gap detection, then NTP
// confirmation when a gap exists. It never panics and never blocks the
// daemon boot (NTP is bounded by its own timeout).
func (g *Guard) Check() Outcome {
	last := g.state.LastKnownTime()
	if last.IsZero() {
		// Primeiro boot (ou estado antigo sem o campo): não há referência
		// para comparar — apenas grava a referência atual e segue.
		_ = g.state.SetLastKnownTime(g.now())
		return Outcome{Detail: "primeira execução: referência do relógio gravada"}
	}

	now := g.now()
	gap := now.Sub(last)
	if gap < 0 {
		gap = -gap
	}
	if gap <= Tolerance {
		// Relógio coerente com a última leitura confiada: grava a nova leitura
		// (a referência desliza com o tempo legítimo) e segue.
		_ = g.state.SetLastKnownTime(now)
		return Outcome{Detail: fmt.Sprintf("relógio consistente (gap %s)", gap.Round(time.Second))}
	}

	// Gap além da tolerância: suspeita. NTP decide.
	out := Outcome{Suspicion: true, Detail: fmt.Sprintf("salto de relógio detectado: |now − lastKnown| = %s (> tolerância %s)", gap.Round(time.Second), Tolerance)}
	if g.ntp == nil {
		out.Detail += " — NTP indisponível: suspeita mantida (sem confirmar nem liberar)"
		return out
	}
	ntpTime, err := g.ntp.Time()
	if err != nil {
		out.Detail += fmt.Sprintf(" — NTP falhou (%v): suspeita mantida", err)
		return out
	}

	realGap := ntpTime.Sub(now)
	if realGap < 0 {
		realGap = -realGap
	}
	if realGap > Tolerance {
		out.Confirmed = true
		out.Detail += fmt.Sprintf(" — NTP confirma burla: relógio local %s fora do real", realGap.Round(time.Second))
		g.applyLockdown(now, ntpTime)
		return out
	}

	// NTP validou o relógio local: era ajuste legítimo (NTP/DST do SO), não
	// burla. Re-ancora a referência no horário real.
	out.Detail += fmt.Sprintf(" — NTP validou o relógio local (diferença real %s): ajuste legítimo", realGap.Round(time.Second))
	_ = g.state.SetLastKnownTime(ntpTime)
	return out
}

// applyLockdown re-ancora as expirações pendentes no horário REAL (o NTP
// acabou de provar qual é) e aplica o bloqueio preventivo. A re-anchoração é
// feita pelo daemon via o hook Ancorador — o guard só aplica o lockdown e
// registra.
func (g *Guard) applyLockdown(_ time.Time, ntpTime time.Time) {
	if g.lock != nil {
		if err := g.lock.BlockAllInternet(nil, g.lockdownDuration); err != nil {
			log.Printf("[ClockGuard] Falha ao aplicar bloqueio preventivo: %v", err)
		}
	}
	if g.logger != nil {
		g.logger.Log("clock", "lockdown",
			fmt.Sprintf("relógio adulterado; bloqueio preventivo até %s; hora real via NTP: %s",
				ntpTime.Add(g.lockdownDuration).Format("2006-01-02 15:04:05"), ntpTime.Format("2006-01-02 15:04:05")))
	}
	// Re-ancora a referência no horário real: o próximo Check compara com o
	// NTP, não com o relógio adulterado.
	_ = g.state.SetLastKnownTime(ntpTime)
}
