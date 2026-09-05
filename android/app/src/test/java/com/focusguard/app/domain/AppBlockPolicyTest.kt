package com.focusguard.app.domain

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

class AppBlockPolicyTest {

    private lateinit var policy: AppBlockPolicy

    @Before
    fun setUp() {
        policy = AppBlockPolicy(
            blockedPackages = setOf(
                "com.instagram.android",
                "com.zhiliaoapp.musically", // TikTok
                "com.twitter.android",
                "com.google.android.youtube"
            )
        )
    }

    @Test
    fun `does not block any app when focus session is inactive`() {
        val shouldBlock = policy.shouldBlock(
            packageName = "com.instagram.android",
            isSessionActive = false
        )
        assertFalse("Should not block apps when session is inactive", shouldBlock)
    }

    @Test
    fun `blocks denylisted apps when focus session is active`() {
        assertTrue(
            "Instagram should be blocked during active session",
            policy.shouldBlock("com.instagram.android", isSessionActive = true)
        )
        assertTrue(
            "TikTok should be blocked during active session",
            policy.shouldBlock("com.zhiliaoapp.musically", isSessionActive = true)
        )
    }

    @Test
    fun `does not block unlisted productive apps during active session`() {
        assertFalse(
            "Productivity apps like Slack or IDEs not on list should not be blocked",
            policy.shouldBlock("com.Slack", isSessionActive = true)
        )
        assertFalse(
            "Calculator should not be blocked",
            policy.shouldBlock("com.google.android.calculator", isSessionActive = true)
        )
    }

    @Test
    fun `never blocks FocusGuard itself or critical system phone dialer`() {
        assertFalse(
            "FocusGuard app itself should never be blocked",
            policy.shouldBlock("com.focusguard.app", isSessionActive = true)
        )
        assertFalse(
            "Phone Dialer should never be blocked (emergency calls)",
            policy.shouldBlock("com.google.android.dialer", isSessionActive = true)
        )
        assertFalse(
            "System UI should never be blocked",
            policy.shouldBlock("com.android.systemui", isSessionActive = true)
        )
    }

    @Test
    fun `detects settings tampering attempt when session is active`() {
        // When user opens settings during active focus to try and uninstall FocusGuard
        val isTampering = policy.isSettingsTamper(
            packageName = "com.android.settings",
            className = "com.android.settings.applications.InstalledAppDetails",
            windowText = listOf("FocusGuard", "Desinstalar", "Forçar parada"),
            isSessionActive = true
        )
        assertTrue("Attempt to uninstall FocusGuard in Settings should be flagged as tamper", isTampering)
    }

    @Test
    fun `does not flag settings when session is inactive`() {
        val isTampering = policy.isSettingsTamper(
            packageName = "com.android.settings",
            className = "com.android.settings.applications.InstalledAppDetails",
            windowText = listOf("FocusGuard", "Desinstalar"),
            isSessionActive = false
        )
        assertFalse("Settings access allowed when no focus session is active", isTampering)
    }

    @Test
    fun `allows dynamic adding and removing of blocked apps`() {
        policy.addBlockedPackage("com.reddit.frontpage")
        assertTrue(policy.shouldBlock("com.reddit.frontpage", isSessionActive = true))

        policy.removeBlockedPackage("com.reddit.frontpage")
        assertFalse(policy.shouldBlock("com.reddit.frontpage", isSessionActive = true))
    }
}
