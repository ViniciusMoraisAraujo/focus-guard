package com.focusguard.app.domain

enum class BreathingPhase {
    INHALE,
    HOLD_IN,
    EXHALE,
    HOLD_OUT
}

data class BreathingState(
    val phase: BreathingPhase,
    val instruction: String,
    val phaseProgress: Float,
    val scale: Float
)

/**
 * BreathingCycle calcula deterministicamente as 4 etapas do exercício
 * de respiração quadrada (Box Breathing 4-4-4-4) com base no tempo decorrido.
 */
class BreathingCycle(
    val phaseDurationSec: Int = 4
) {
    private val phaseDurationMs = phaseDurationSec * 1000L
    private val totalCycleMs = phaseDurationMs * 4L

    fun getPhaseAt(elapsedMs: Long): BreathingState {
        val cycleTime = elapsedMs % totalCycleMs
        val phaseIndex = (cycleTime / phaseDurationMs).toInt()
        val phaseElapsed = cycleTime % phaseDurationMs
        val progress = phaseElapsed.toFloat() / phaseDurationMs.toFloat()

        val (phase, instruction) = when (phaseIndex) {
            0 -> BreathingPhase.INHALE to "Inspire pelo nariz"
            1 -> BreathingPhase.HOLD_IN to "Segure o ar"
            2 -> BreathingPhase.EXHALE to "Expire pela boca"
            else -> BreathingPhase.HOLD_OUT to "Mantenha vazio"
        }

        val scale = getScaleAt(elapsedMs)
        return BreathingState(phase, instruction, progress, scale)
    }

    fun getScaleAt(elapsedMs: Long): Float {
        val cycleTime = elapsedMs % totalCycleMs
        val phaseIndex = (cycleTime / phaseDurationMs).toInt()
        val phaseElapsed = cycleTime % phaseDurationMs
        val progress = phaseElapsed.toFloat() / phaseDurationMs.toFloat()

        return when (phaseIndex) {
            0 -> 1.0f + (0.4f * progress) // 1.0 -> 1.4
            1 -> 1.4f                     // 1.4 estável
            2 -> 1.4f - (0.4f * progress) // 1.4 -> 1.0
            else -> 1.0f                  // 1.0 estável
        }
    }
}
