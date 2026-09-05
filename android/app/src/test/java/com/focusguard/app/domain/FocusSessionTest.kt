package com.focusguard.app.domain

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

class FocusSessionTest {

    private var currentTimeMs: Long = 1000L
    private lateinit var session: FocusSession

    @Before
    fun setUp() {
        currentTimeMs = 1000L
        session = FocusSession(clock = { currentTimeMs })
    }

    @Test
    fun `session starts inactive`() {
        assertFalse("Session should initially be inactive", session.isActive())
        assertEquals("Remaining time should be 0", 0L, session.remainingTimeMs())
        assertTrue("Can cancel/uninstall when session is inactive", session.canCancel())
    }

    @Test
    fun `start activates session for specified duration`() {
        val durationMs = 25 * 60 * 1000L // 25 minutes
        session.start(durationMs = durationMs)

        assertTrue("Session should be active", session.isActive())
        assertEquals("Remaining time should match duration", durationMs, session.remainingTimeMs())
        assertFalse("Cannot cancel/stop during active session (anti-burla)", session.canCancel())
    }

    @Test
    fun `remaining time decreases as clock advances`() {
        val durationMs = 30 * 60 * 1000L // 30 minutes
        session.start(durationMs = durationMs)

        // Advance 10 minutes
        currentTimeMs += 10 * 60 * 1000L

        assertTrue("Session should still be active", session.isActive())
        assertEquals(
            "Remaining time should be 20 minutes",
            20 * 60 * 1000L,
            session.remainingTimeMs()
        )
    }

    @Test
    fun `session expires when duration elapses`() {
        val durationMs = 15 * 60 * 1000L // 15 minutes
        session.start(durationMs = durationMs)

        // Advance 15 minutes and 1 millisecond
        currentTimeMs += durationMs + 1L

        assertFalse("Session should be expired/inactive", session.isActive())
        assertEquals("Remaining time should be 0", 0L, session.remainingTimeMs())
        assertTrue("Can cancel/uninstall once session has expired", session.canCancel())
    }

    @Test(expected = IllegalStateException::class)
    fun `stop throws error if attempted during active session`() {
        session.start(durationMs = 10 * 60 * 1000L)
        // Anti-tamper rule: stopping during active session is forbidden!
        session.stop()
    }

    @Test
    fun `stop succeeds when session is already inactive or expired`() {
        val durationMs = 5 * 60 * 1000L
        session.start(durationMs = durationMs)
        currentTimeMs += durationMs + 10L // Expired

        // Should not throw
        session.stop()
        assertFalse(session.isActive())
    }
}
