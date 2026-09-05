package com.focusguard.app.domain

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

class PomodoroEngineTest {

    private var currentTimeMs = 1000L
    private val workDurationMs = 25 * 60 * 1000L // 25 min
    private val restDurationMs = 5 * 60 * 1000L  // 5 min
    private val longRestDurationMs = 15 * 60 * 1000L // 15 min

    private lateinit var engine: PomodoroEngine

    @Before
    fun setUp() {
        currentTimeMs = 1000L
        engine = PomodoroEngine(
            workDurationMs = workDurationMs,
            restDurationMs = restDurationMs,
            longRestDurationMs = longRestDurationMs,
            clock = { currentTimeMs }
        )
    }

    @Test
    fun `initial state is IDLE`() {
        val state = engine.getState()
        assertEquals(PomodoroPhase.IDLE, state.phase)
        assertEquals(0, state.completedCycles)
        assertEquals(0L, state.remainingTimeMs)
        assertFalse(engine.isBlockingActive())
        assertTrue(engine.canCancel())
    }

    @Test
    fun `start starts WORK phase with mission`() {
        engine.start(mission = "Refatorar Módulo de Rede")

        val state = engine.getState()
        assertEquals(PomodoroPhase.WORK, state.phase)
        assertEquals("Refatorar Módulo de Rede", state.mission)
        assertEquals(workDurationMs, state.remainingTimeMs)
        assertEquals(1, state.currentCycleIndex)
        assertTrue(engine.isBlockingActive())
        assertFalse("Cannot cancel during active work phase (anti-burla)", engine.canCancel())
    }

    @Test
    fun `remaining time decreases during WORK phase`() {
        engine.start(mission = "Estudos")

        // Advance 10 minutes
        currentTimeMs += 10 * 60 * 1000L

        val state = engine.getState()
        assertEquals(PomodoroPhase.WORK, state.phase)
        assertEquals(15 * 60 * 1000L, state.remainingTimeMs)
    }

    @Test
    fun `when WORK expires transitions to REST`() {
        engine.start(mission = "Estudos")

        // Advance 25 minutes + 1ms
        currentTimeMs += workDurationMs + 1L

        val state = engine.getState()
        assertEquals(PomodoroPhase.REST, state.phase)
        assertEquals(1, state.completedCycles)
        assertEquals(restDurationMs, state.remainingTimeMs)
        assertFalse("Blocking is disabled during rest period", engine.isBlockingActive())
        assertTrue("Can pause or stop during rest period", engine.canCancel())
    }

    @Test
    fun `transitions to LONG_REST after 4th cycle`() {
        // Complete 3 cycles
        for (i in 1..3) {
            engine.start(mission = "Ciclo $i")
            currentTimeMs += workDurationMs + 1L // Transitions to REST
            assertEquals(PomodoroPhase.REST, engine.getState().phase)
            currentTimeMs += restDurationMs + 1L // Rest expires
            assertEquals(PomodoroPhase.IDLE, engine.getState().phase)
        }

        // Start 4th cycle
        engine.start(mission = "Ciclo 4")
        currentTimeMs += workDurationMs + 1L // Work expires

        val state = engine.getState()
        assertEquals(PomodoroPhase.LONG_REST, state.phase)
        assertEquals(4, state.completedCycles)
        assertEquals(longRestDurationMs, state.remainingTimeMs)
        assertFalse(engine.isBlockingActive())
    }

    @Test(expected = IllegalStateException::class)
    fun `stopping during WORK phase is forbidden by anti-burla`() {
        engine.start(mission = "Foco Estrito")
        // Should throw IllegalStateException
        engine.stop()
    }

    @Test
    fun `stopping during REST phase is allowed`() {
        engine.start(mission = "Foco")
        currentTimeMs += workDurationMs + 1L // In REST phase

        engine.stop()
        assertEquals(PomodoroPhase.IDLE, engine.getState().phase)
    }

    @Test
    fun `skipBreak immediately starts next WORK cycle`() {
        engine.start(mission = "Foco")
        currentTimeMs += workDurationMs + 1L // In REST phase
        assertEquals(PomodoroPhase.REST, engine.getState().phase)

        engine.skipBreak()
        val state = engine.getState()
        assertEquals(PomodoroPhase.WORK, state.phase)
        assertEquals(2, state.currentCycleIndex)
        assertTrue(engine.isBlockingActive())
    }
}
