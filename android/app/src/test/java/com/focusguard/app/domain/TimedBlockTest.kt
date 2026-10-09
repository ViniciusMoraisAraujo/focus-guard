package com.focusguard.app.domain

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class TimedBlockTest {

    @Test
    fun `is active when current time is before expiration`() {
        val block = TimedBlock(
            id = "com.instagram.android",
            targetType = BlockTargetType.APP,
            targetValue = "com.instagram.android",
            displayName = "Instagram",
            durationMs = 60_000L,
            startedAtElapsedMs = 1000L
        )

        assertTrue(block.isActive(nowElapsedMs = 1000L))
        assertTrue(block.isActive(nowElapsedMs = 30_000L))
        assertTrue(block.isActive(nowElapsedMs = 60_999L))
        assertFalse(block.isActive(nowElapsedMs = 61_000L))
        assertFalse(block.isActive(nowElapsedMs = 70_000L))
    }

    @Test
    fun `remaining time decreases as time elapses`() {
        val block = TimedBlock(
            id = "com.instagram.android",
            targetType = BlockTargetType.APP,
            targetValue = "com.instagram.android",
            displayName = "Instagram",
            durationMs = 60_000L,
            startedAtElapsedMs = 0L
        )

        assertEquals(60_000L, block.remainingTimeMs(nowElapsedMs = 0L))
        assertEquals(40_000L, block.remainingTimeMs(nowElapsedMs = 20_000L))
        assertEquals(0L, block.remainingTimeMs(nowElapsedMs = 60_000L))
        assertEquals(0L, block.remainingTimeMs(nowElapsedMs = 90_000L))
    }

    @Test
    fun `extend increases duration and expiration time`() {
        val block = TimedBlock(
            id = "social",
            targetType = BlockTargetType.PRESET,
            targetValue = "social",
            displayName = "Redes Sociais",
            durationMs = 30_000L,
            startedAtElapsedMs = 0L
        )

        assertEquals(30_000L, block.expiresAtElapsedMs)

        block.extend(extraMs = 15_000L)
        assertEquals(45_000L, block.expiresAtElapsedMs)
        assertEquals(45_000L, block.durationMs)
    }

    @Test(expected = IllegalArgumentException::class)
    fun `rejects non-positive duration`() {
        TimedBlock(
            id = "test",
            targetType = BlockTargetType.APP,
            targetValue = "test.app",
            displayName = "Test",
            durationMs = 0L,
            startedAtElapsedMs = 0L
        )
    }
}
