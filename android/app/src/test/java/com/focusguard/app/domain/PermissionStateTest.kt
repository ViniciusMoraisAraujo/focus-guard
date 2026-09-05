package com.focusguard.app.domain

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class PermissionStateTest {

    @Test
    fun `initial state with no permissions granted`() {
        val state = PermissionStatus(
            accessibilityGranted = false,
            overlayGranted = false,
            vpnGranted = false
        )

        assertFalse("All should not be granted", state.isAllGranted())
        assertEquals("Granted count should be 0", 0, state.grantedCount())
        assertEquals("Progress should be 0%", 0f, state.progress(), 0.01f)
        assertEquals("Missing 3 permissions", 3, state.missingPermissions().size)
    }

    @Test
    fun `partial permissions granted updates progress correctly`() {
        val state = PermissionStatus(
            accessibilityGranted = true,
            overlayGranted = false,
            vpnGranted = false
        )

        assertFalse(state.isAllGranted())
        assertEquals(1, state.grantedCount())
        assertEquals(0.333f, state.progress(), 0.01f)
        assertTrue(state.missingPermissions().contains(PermissionType.OVERLAY))
        assertTrue(state.missingPermissions().contains(PermissionType.VPN))
        assertFalse(state.missingPermissions().contains(PermissionType.ACCESSIBILITY))
    }

    @Test
    fun `all permissions granted allows proceeding`() {
        val state = PermissionStatus(
            accessibilityGranted = true,
            overlayGranted = true,
            vpnGranted = true
        )

        assertTrue("All permissions are granted", state.isAllGranted())
        assertEquals(3, state.grantedCount())
        assertEquals(1.0f, state.progress(), 0.01f)
        assertTrue(state.missingPermissions().isEmpty())
    }
}
