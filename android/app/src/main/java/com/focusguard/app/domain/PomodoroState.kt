package com.focusguard.app.domain

enum class PomodoroPhase {
    IDLE,
    WORK,
    REST,
    LONG_REST
}

data class PomodoroState(
    val phase: PomodoroPhase,
    val currentCycleIndex: Int,
    val completedCycles: Int,
    val mission: String,
    val remainingTimeMs: Long,
    val totalPhaseDurationMs: Long
) {
    val progress: Float
        get() {
            if (totalPhaseDurationMs <= 0L) return 0f
            val elapsed = totalPhaseDurationMs - remainingTimeMs
            return (elapsed.toFloat() / totalPhaseDurationMs.toFloat()).coerceIn(0f, 1f)
        }
}
