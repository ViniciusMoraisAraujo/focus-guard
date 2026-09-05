package com.focusguard.app.domain

import android.accessibilityservice.AccessibilityServiceInfo
import android.content.Context
import android.net.VpnService
import android.os.Build
import android.provider.Settings
import android.view.accessibility.AccessibilityManager
import com.focusguard.app.service.FocusAccessibilityService

enum class PermissionType {
    ACCESSIBILITY,
    OVERLAY,
    VPN
}

data class PermissionStatus(
    val accessibilityGranted: Boolean,
    val overlayGranted: Boolean,
    val vpnGranted: Boolean
) {
    fun isAllGranted(): Boolean = accessibilityGranted && overlayGranted && vpnGranted

    fun grantedCount(): Int {
        var count = 0
        if (accessibilityGranted) count++
        if (overlayGranted) count++
        if (vpnGranted) count++
        return count
    }

    fun progress(): Float = grantedCount().toFloat() / 3.0f

    fun missingPermissions(): List<PermissionType> {
        val missing = mutableListOf<PermissionType>()
        if (!accessibilityGranted) missing.add(PermissionType.ACCESSIBILITY)
        if (!overlayGranted) missing.add(PermissionType.OVERLAY)
        if (!vpnGranted) missing.add(PermissionType.VPN)
        return missing
    }
}

object PermissionManager {

    fun checkPermissions(context: Context): PermissionStatus {
        return PermissionStatus(
            accessibilityGranted = isAccessibilityGranted(context),
            overlayGranted = isOverlayGranted(context),
            vpnGranted = isVpnGranted(context)
        )
    }

    fun isAccessibilityGranted(context: Context): Boolean {
        val am = context.getSystemService(Context.ACCESSIBILITY_SERVICE) as? AccessibilityManager ?: return false
        val enabledServices = am.getEnabledAccessibilityServiceList(AccessibilityServiceInfo.FEEDBACK_ALL_MASK)
        val expectedServiceName = "${context.packageName}/${FocusAccessibilityService::class.java.name}"
        return enabledServices.any { serviceInfo ->
            val id = serviceInfo.id
            id.equals(expectedServiceName, ignoreCase = true) || id.contains(FocusAccessibilityService::class.java.simpleName)
        }
    }

    fun isOverlayGranted(context: Context): Boolean {
        return if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
            Settings.canDrawOverlays(context)
        } else {
            true
        }
    }

    fun isVpnGranted(context: Context): Boolean {
        // VpnService.prepare retorna null se a permissão já foi concedida anteriormente!
        return VpnService.prepare(context) == null
    }
}
