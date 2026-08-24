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
//  3. When NTP CONFIRMS the discrepancy, the guard knows the real time, so
//     it logs the confirmed discrepancy (deduped per offset), re-anchors the
//     reference on the real time. The daemon additionally shifts the pending
//     expirations to real time before the boot reconcile. This keeps
//     dual-boot users (Windows RTC em hora local × Linux RTC em UTC) usable:
//     a persistently offset clock is a configuration issue, not an attack.
//     NTP validating the local clock re-anchors the reference.
package clockguard

import (
	"fmt"
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
// *scheduler.Scheduler): the last trusted reading.
type State interface {
	LastKnownTime() time.Time
	SetLastKnownTime(t time.Time) error
}

// Logger records a confirmed tamper (the daemon wires it to the tamper
// recorder — a no-op logger keeps the guard usable in tests).
type Logger interface {
	Log(source, action, detail string)
}

// logDedupTolerance is how much the confirmed offset must change between
// Checks for a new tamper-log entry. A persistently offset clock (dual boot,
// RTC wrong time standard) is confirmed at every Check — without this, the
// tamper-log would fill with one entry every CheckInterval for a machine
// whose user did nothing wrong. A NEW jump (offset moved beyond tolerance)
// is a new event worth recording.
const logDedupTolerance = Tolerance

// Guard runs the clock-tamper detection. All dependencies are injectable so
// tests exercise every branch without a real clock or network.
type Guard struct {
	state  State
	ntp    NTPClient
	now    func() time.Time
	logger Logger
	// lastLoggedOffset é o offset (local − real) do último evento de
	// divergência confirmada registrado no tamper-log — dedup para um
	// relógio persistentemente fora (dual boot) não espalhar o log a cada
	// Check. Vive só em memória: a cada boot um novo offset é registrado
	// uma vez.
	lastLoggedOffset time.Duration
	hasLoggedOffset  bool
}

// Deps wires a guard. ntp may be nil (offline daemon): a nil NTP client makes
// Check treat any suspicion as UNRESOLVABLE instead of being validated/cleared.
type Deps struct {
	State  State
	NTP    NTPClient
	Now    func() time.Time
	Logger Logger
}

// New builds a guard with the dependencies. A nil Now falls back to
// time.Now.
func New(d Deps) *Guard {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	return &Guard{
		state: d.State, ntp: d.NTP,
		now: now, logger: d.Logger,
	}
}

// Outcome reports what a Check run decided.
type Outcome struct {
	// Suspicion true = the wall clock jumped beyond tolerance.
	Suspicion bool
	// Confirmed true = NTP agreed the local clock is wrong (tamper).
	Confirmed bool
	// Offset is the confirmed local−real clock offset (local.Sub(real)),
	// meaningful only when Confirmed. Positive when the local clock is AHEAD
	// of real time. The daemon shifts the pending expirations by this offset
	// (scheduler.ShiftExpirations) BEFORE the boot reconcile, so a boot with
	// a forged clock neither expires blocks early nor holds them past their
	// real expiry.
	Offset time.Duration
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

	// Gap além da tolerância: suspeita. Com NTP indisponível a suspeita não
	// pode ser resolvida. Com NTP disponível, o veredito dele decide: valida
	// → sem bloqueio; confirma → registra, re-ancora e ajusta.
	out := Outcome{Suspicion: true, Detail: fmt.Sprintf("salto de relógio detectado: |now − lastKnown| = %s (> tolerância %s)", gap.Round(time.Second), Tolerance)}
	if g.ntp == nil {
		out.Detail += " — NTP indisponível"
		return out
	}
	ntpTime, err := g.ntp.Time()
	if err != nil {
		out.Detail += fmt.Sprintf(" — NTP falhou (%v)", err)
		return out
	}

	realGap := ntpTime.Sub(now)
	if realGap < 0 {
		realGap = -realGap
	}
	if realGap > Tolerance {
		out.Confirmed = true
		out.Offset = now.Sub(ntpTime) // local − real: positivo = relógio à frente
		out.Detail += fmt.Sprintf(" — NTP confirma divergência: relógio local %s fora do real; expirações ajustadas para a hora real", realGap.Round(time.Second))
		g.recordConfirmed(ntpTime, out.Offset)
		return out
	}

	// NTP validou o relógio local: era ajuste legítimo (NTP/DST do SO), não
	// burla. Re-ancora a referência no horário real.
	out.Detail += fmt.Sprintf(" — NTP validou o relógio local (diferença real %s): ajuste legítimo", realGap.Round(time.Second))
	_ = g.state.SetLastKnownTime(ntpTime)
	return out
}

// recordConfirmed registra a divergência confirmada por NTP no tamper-log
// (dedup por offset — um relógio persistentemente fora, como no dual boot,
// não gera um evento a cada Check), re-ancora a referência no horário REAL.
// NÃO aplica lockdown: com o NTP respondendo, o guard sabe a hora real e as
// expirações são ajustadas para ela (ShiftExpirations, no boot) — bloquear
// toda a internet seria punir um relógio do SO configurado errado (RTC/fuso,
// dual boot).
func (g *Guard) recordConfirmed(ntpTime time.Time, offset time.Duration) {
	if changed := !g.hasLoggedOffset || absDuration(offset-g.lastLoggedOffset) > logDedupTolerance; changed && g.logger != nil {
		dir := "à frente"
		if offset < 0 {
			dir = "atrás"
		}
		g.logger.Log("clock", "lockdown",
			fmt.Sprintf("relógio local %s %s do real, confirmado por NTP; expirações ajustadas para a hora real (%s); verifique o RTC/fuso (ex.: dual boot)",
				absDuration(offset).Round(time.Second), dir, ntpTime.Format("2006-01-02 15:04:05")))
		g.lastLoggedOffset = offset
		g.hasLoggedOffset = true
	}
	// Re-ancora a referência no horário real: o próximo Check compara com o
	// NTP, não com o relógio adulterado.
	_ = g.state.SetLastKnownTime(ntpTime)
}

// absDuration devolve |d|.
func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
