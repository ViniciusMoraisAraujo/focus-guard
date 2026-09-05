package com.focusguard.app.domain

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

class PresetManagerTest {

    private var isSessionActive = false
    private lateinit var presetManager: PresetManager

    @Before
    fun setUp() {
        isSessionActive = false
        presetManager = PresetManager(isSessionActiveProvider = { isSessionActive })
    }

    @Test
    fun `default presets include Social and Streaming`() {
        val presets = presetManager.getPresets()
        val social = presets.find { it.id == "social" }
        val streaming = presets.find { it.id == "streaming" }

        assertTrue("Social preset should exist", social != null)
        assertTrue("Streaming preset should exist", streaming != null)

        assertTrue(
            "Social preset should block Instagram package",
            social!!.apps.contains("com.instagram.android")
        )
        assertTrue(
            "Social preset should block instagram.com domain",
            social.domains.contains("instagram.com")
        )
        assertTrue(
            "Streaming preset should block YouTube package",
            streaming!!.apps.contains("com.google.android.youtube")
        )
    }

    @Test
    fun `enabling and disabling preset updates blocked packages and domains`() {
        // Initially enabled
        assertTrue(presetManager.isPackageBlocked("com.instagram.android"))
        assertTrue(presetManager.isDomainBlocked("instagram.com"))

        // Disable Social preset when session is inactive
        presetManager.setPresetEnabled("social", enabled = false)

        assertFalse(presetManager.isPackageBlocked("com.instagram.android"))
        assertFalse(presetManager.isDomainBlocked("instagram.com"))

        // Re-enable
        presetManager.setPresetEnabled("social", enabled = true)
        assertTrue(presetManager.isPackageBlocked("com.instagram.android"))
    }

    @Test(expected = IllegalStateException::class)
    fun `disabling a preset during active focus session is forbidden by anti-burla`() {
        isSessionActive = true
        // Trying to turn off social preset while focus is active!
        presetManager.setPresetEnabled("social", enabled = false)
    }

    @Test
    fun `enabling a preset during active focus session is allowed`() {
        presetManager.setPresetEnabled("streaming", enabled = false)
        isSessionActive = true

        // User decides to block MORE distractions during focus -> allowed!
        presetManager.setPresetEnabled("streaming", enabled = true)
        assertTrue(presetManager.isPackageBlocked("com.google.android.youtube"))
    }

    @Test(expected = IllegalStateException::class)
    fun `unblocking a custom app during active focus session is forbidden by anti-burla`() {
        presetManager.setCustomAppBlocked("com.custom.game", blocked = true)
        isSessionActive = true

        // Trying to unblock custom app during focus session
        presetManager.setCustomAppBlocked("com.custom.game", blocked = false)
    }

    @Test
    fun `custom apps can be added and combine with active presets`() {
        presetManager.setCustomAppBlocked("com.reddit.frontpage", blocked = true)
        assertTrue(presetManager.isPackageBlocked("com.reddit.frontpage"))

        // Even if social preset is disabled, custom app remains blocked
        presetManager.setPresetEnabled("social", enabled = false)
        assertTrue(presetManager.isPackageBlocked("com.reddit.frontpage"))
        assertFalse(presetManager.isPackageBlocked("com.instagram.android"))
    }
}
