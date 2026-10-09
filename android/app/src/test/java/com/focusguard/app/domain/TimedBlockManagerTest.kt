package com.focusguard.app.domain

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNotNull
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

class TimedBlockManagerTest {

    private var currentClockMs = 1000L
    private lateinit var presetManager: PresetManager
    private lateinit var timedBlockManager: TimedBlockManager

    @Before
    fun setUp() {
        currentClockMs = 1000L
        presetManager = PresetManager(isSessionActiveProvider = { false })
        timedBlockManager = TimedBlockManager(clock = { currentClockMs })
    }

    @Test
    fun `starts with no active blocks`() {
        assertFalse(timedBlockManager.hasActiveBlocks())
        assertTrue(timedBlockManager.getActiveBlocks().isEmpty())
        assertEquals(0L, timedBlockManager.maxRemainingTimeMs())
    }

    @Test
    fun `blockApp adds active app block`() {
        val block = timedBlockManager.blockApp(
            packageName = "com.instagram.android",
            displayName = "Instagram",
            durationMs = 60_000L
        )

        assertEquals("com.instagram.android", block.id)
        assertEquals(BlockTargetType.APP, block.targetType)
        assertTrue(timedBlockManager.hasActiveBlocks())
        assertTrue(timedBlockManager.isPackageBlocked("com.instagram.android", presetManager))
        assertFalse(timedBlockManager.isPackageBlocked("com.twitter.android", presetManager))
        assertEquals(60_000L, timedBlockManager.getRemainingTimeForPackage("com.instagram.android", presetManager))
    }

    @Test
    fun `blockPreset blocks all apps belonging to preset`() {
        // Preset 'social' contains Instagram, TikTok, Twitter/X, etc.
        timedBlockManager.blockPreset(
            presetId = "social",
            displayName = "Redes Sociais",
            durationMs = 120_000L
        )

        assertTrue(timedBlockManager.isPackageBlocked("com.instagram.android", presetManager))
        assertTrue(timedBlockManager.isPackageBlocked("com.zhiliaoapp.musically", presetManager))
        assertTrue(timedBlockManager.isPackageBlocked("com.twitter.android", presetManager))
        // YouTube is in streaming, not social
        assertFalse(timedBlockManager.isPackageBlocked("com.google.android.youtube", presetManager))
        assertEquals(120_000L, timedBlockManager.getRemainingTimeForPackage("com.instagram.android", presetManager))
    }

    @Test
    fun `blocks expire when clock advances beyond duration`() {
        timedBlockManager.blockApp(
            packageName = "com.instagram.android",
            displayName = "Instagram",
            durationMs = 30_000L
        )

        assertTrue(timedBlockManager.isPackageBlocked("com.instagram.android", presetManager))

        // Advance clock past expiration
        currentClockMs += 30_001L

        assertFalse(timedBlockManager.isPackageBlocked("com.instagram.android", presetManager))
        assertFalse(timedBlockManager.hasActiveBlocks())
        assertEquals(0L, timedBlockManager.getRemainingTimeForPackage("com.instagram.android", presetManager))
    }

    @Test
    fun `extendBlock increases duration of active block`() {
        timedBlockManager.blockApp(
            packageName = "com.instagram.android",
            displayName = "Instagram",
            durationMs = 30_000L
        )

        // Advance 10s
        currentClockMs += 10_000L

        // Extend by 20s
        val updated = timedBlockManager.extendBlock(
            id = "com.instagram.android",
            extraMs = 20_000L
        )

        assertNotNull(updated)
        // Original 30s - 10s elapsed + 20s extra = 40s remaining
        assertEquals(40_000L, timedBlockManager.getRemainingTimeForPackage("com.instagram.android", presetManager))
    }

    @Test(expected = IllegalStateException::class)
    fun `unblocking active block before expiration throws exception due to anti-burla`() {
        timedBlockManager.blockApp(
            packageName = "com.instagram.android",
            displayName = "Instagram",
            durationMs = 60_000L
        )

        // Attempting to cancel/remove while active MUST be rejected
        timedBlockManager.cancelBlock("com.instagram.android")
    }

    @Test
    fun `unblocking after expiration succeeds`() {
        timedBlockManager.blockApp(
            packageName = "com.instagram.android",
            displayName = "Instagram",
            durationMs = 10_000L
        )

        currentClockMs += 10_001L
        timedBlockManager.cancelBlock("com.instagram.android")
        assertFalse(timedBlockManager.hasActiveBlocks())
    }

    @Test
    fun `multiple blocks report correct maximum remaining time`() {
        timedBlockManager.blockApp("com.instagram.android", "Instagram", 30_000L)
        timedBlockManager.blockApp("com.google.android.youtube", "YouTube", 60_000L)

        assertEquals(60_000L, timedBlockManager.maxRemainingTimeMs())

        currentClockMs += 35_000L
        // Instagram expired, YouTube has 25s remaining
        assertEquals(25_000L, timedBlockManager.maxRemainingTimeMs())
    }
}
