package com.focusguard.app.domain

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class AppSearchTest {

    private val sampleApps = listOf(
        AppItem(packageName = "com.instagram.android", appName = "Instagram", isBlocked = true),
        AppItem(packageName = "com.zhiliaoapp.musically", appName = "TikTok", isBlocked = true),
        AppItem(packageName = "com.google.android.youtube", appName = "YouTube", isBlocked = false),
        AppItem(packageName = "com.whatsapp", appName = "WhatsApp", isBlocked = false),
        AppItem(packageName = "com.spotify.music", appName = "Spotify", isBlocked = false)
    )

    @Test
    fun `empty query returns all apps`() {
        val result = AppSearchFilter.filter(sampleApps, query = "")
        assertEquals(5, result.size)
    }

    @Test
    fun `blank or whitespace query returns all apps`() {
        val result = AppSearchFilter.filter(sampleApps, query = "   ")
        assertEquals(5, result.size)
    }

    @Test
    fun `query matches app name case-insensitively`() {
        val result = AppSearchFilter.filter(sampleApps, query = "insta")
        assertEquals(1, result.size)
        assertEquals("Instagram", result.first().appName)

        val resultUpper = AppSearchFilter.filter(sampleApps, query = "TIKTOK")
        assertEquals(1, resultUpper.size)
        assertEquals("TikTok", resultUpper.first().appName)
    }

    @Test
    fun `query matches package name`() {
        // "musically" matches package com.zhiliaoapp.musically
        val result = AppSearchFilter.filter(sampleApps, query = "musically")
        assertEquals(1, result.size)
        assertEquals("TikTok", result.first().appName)
    }

    @Test
    fun `query with no matches returns empty list`() {
        val result = AppSearchFilter.filter(sampleApps, query = "nonexistent_app_xyz")
        assertTrue(result.isEmpty())
    }
}
