package com.focusguard.app.domain

import org.junit.Assert.assertEquals
import org.junit.Test

class BreathingCycleTest {

    private val cycle = BreathingCycle(phaseDurationSec = 4)

    @Test
    fun `cycle phase at start is INHALE`() {
        val state = cycle.getPhaseAt(elapsedMs = 0L)
        assertEquals(BreathingPhase.INHALE, state.phase)
        assertEquals("Inspire pelo nariz", state.instruction)
        assertEquals(0f, state.phaseProgress, 0.01f)
    }

    @Test
    fun `cycle phase at 2 seconds is mid INHALE`() {
        val state = cycle.getPhaseAt(elapsedMs = 2000L)
        assertEquals(BreathingPhase.INHALE, state.phase)
        assertEquals(0.5f, state.phaseProgress, 0.01f)
    }

    @Test
    fun `cycle phase at 4 seconds transitions to HOLD_IN`() {
        val state = cycle.getPhaseAt(elapsedMs = 4000L)
        assertEquals(BreathingPhase.HOLD_IN, state.phase)
        assertEquals("Segure o ar", state.instruction)
        assertEquals(0f, state.phaseProgress, 0.01f)
    }

    @Test
    fun `cycle phase at 8 seconds transitions to EXHALE`() {
        val state = cycle.getPhaseAt(elapsedMs = 8000L)
        assertEquals(BreathingPhase.EXHALE, state.phase)
        assertEquals("Expire pela boca", state.instruction)
        assertEquals(0f, state.phaseProgress, 0.01f)
    }

    @Test
    fun `cycle phase at 12 seconds transitions to HOLD_OUT`() {
        val state = cycle.getPhaseAt(elapsedMs = 12000L)
        assertEquals(BreathingPhase.HOLD_OUT, state.phase)
        assertEquals("Mantenha vazio", state.instruction)
        assertEquals(0f, state.phaseProgress, 0.01f)
    }

    @Test
    fun `cycle loops seamlessly after 16 seconds`() {
        // 16s + 1s = 17s (should be 1s into INHALE of next cycle)
        val state = cycle.getPhaseAt(elapsedMs = 17000L)
        assertEquals(BreathingPhase.INHALE, state.phase)
        assertEquals(0.25f, state.phaseProgress, 0.01f)
    }

    @Test
    fun `scale factor expands during inhale and shrinks during exhale`() {
        // Start: scale is 1.0f
        assertEquals(1.0f, cycle.getScaleAt(0L), 0.01f)
        // Mid-inhale: scale is 1.2f
        assertEquals(1.2f, cycle.getScaleAt(2000L), 0.01f)
        // Hold-in: scale stays at 1.4f
        assertEquals(1.4f, cycle.getScaleAt(5000L), 0.01f)
        // Mid-exhale: scale shrinks back to 1.2f
        assertEquals(1.2f, cycle.getScaleAt(10000L), 0.01f)
        // Hold-out: scale stays at 1.0f
        assertEquals(1.0f, cycle.getScaleAt(14000L), 0.01f)
    }
}
