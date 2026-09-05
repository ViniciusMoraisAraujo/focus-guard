package com.focusguard.app.domain

import android.os.SystemClock

/**
 * PomodoroEngine implementa o motor determinístico da técnica Pomodoro
 * com alternância automática entre foco e descanso, missões e proteção anti-burla.
 */
class PomodoroEngine(
    val workDurationMs: Long = 25 * 60 * 1000L,
    val restDurationMs: Long = 5 * 60 * 1000L,
    val longRestDurationMs: Long = 15 * 60 * 1000L,
    private val clock: () -> Long = { SystemClock.elapsedRealtime() }
) {
    private var phase: PomodoroPhase = PomodoroPhase.IDLE
    private var currentCycleIndex: Int = 0
    private var completedCycles: Int = 0
    private var mission: String = ""
    private var phaseExpiresAtMs: Long = 0L
    private var phaseTotalMs: Long = 0L

    fun getState(): PomodoroState {
        updatePhaseTransitionsIfNeeded()

        val now = clock()
        val remaining = if (phaseExpiresAtMs > now) phaseExpiresAtMs - now else 0L

        return PomodoroState(
            phase = phase,
            currentCycleIndex = currentCycleIndex,
            completedCycles = completedCycles,
            mission = mission,
            remainingTimeMs = remaining,
            totalPhaseDurationMs = phaseTotalMs
        )
    }

    fun start(mission: String = "") {
        updatePhaseTransitionsIfNeeded()
        if (mission.isNotBlank()) {
            this.mission = mission
        }
        currentCycleIndex = completedCycles + 1
        phase = PomodoroPhase.WORK
        phaseTotalMs = workDurationMs
        phaseExpiresAtMs = clock() + workDurationMs
    }

    fun canCancel(): Boolean {
        updatePhaseTransitionsIfNeeded()
        return phase != PomodoroPhase.WORK
    }

    fun stop() {
        check(canCancel()) {
            "Não é permitido interromper o Pomodoro durante a fase ativa de trabalho!"
        }
        phase = PomodoroPhase.IDLE
        phaseExpiresAtMs = 0L
        phaseTotalMs = 0L
    }

    fun skipBreak() {
        updatePhaseTransitionsIfNeeded()
        if (phase == PomodoroPhase.REST || phase == PomodoroPhase.LONG_REST) {
            start(mission)
        }
    }

    fun isBlockingActive(): Boolean {
        updatePhaseTransitionsIfNeeded()
        return phase == PomodoroPhase.WORK
    }

    private fun updatePhaseTransitionsIfNeeded() {
        if (phase == PomodoroPhase.IDLE) return

        val now = clock()
        if (now >= phaseExpiresAtMs) {
            when (phase) {
                PomodoroPhase.WORK -> {
                    completedCycles++
                    if (completedCycles % 4 == 0) {
                        phase = PomodoroPhase.LONG_REST
                        phaseTotalMs = longRestDurationMs
                    } else {
                        phase = PomodoroPhase.REST
                        phaseTotalMs = restDurationMs
                    }
                    phaseExpiresAtMs = now + phaseTotalMs
                }
                PomodoroPhase.REST, PomodoroPhase.LONG_REST -> {
                    phase = PomodoroPhase.IDLE
                    phaseExpiresAtMs = 0L
                    phaseTotalMs = 0L
                }
                PomodoroPhase.IDLE -> {}
            }
        }
    }
}
