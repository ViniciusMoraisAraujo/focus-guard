package com.focusguard.app.domain

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * FocusGuardManager é o ponto central de estado compartilhado em memória
 * entre a UI Jetpack Compose e os Serviços do Android.
 */
object FocusGuardManager {
    val session: FocusSession = FocusSession()

    val presetManager: PresetManager = PresetManager(
        isSessionActiveProvider = { session.isActive() }
    )

    val appPolicy: AppBlockPolicy = AppBlockPolicy(
        blockedPackages = presetManager.getAllBlockedPackages()
    )

    val dnsPolicy: DnsBlockPolicy = DnsBlockPolicy(
        blockedDomains = presetManager.getAllBlockedDomains()
    )

    val pomodoroEngine: PomodoroEngine = PomodoroEngine()

    private val _isFocusActive = MutableStateFlow(false)
    val isFocusActive: StateFlow<Boolean> = _isFocusActive.asStateFlow()

    fun syncPolicies() {
        val packages = presetManager.getAllBlockedPackages()
        // Atualiza a política do enforcer de apps
        for (pkg in packages) {
            appPolicy.addBlockedPackage(pkg)
        }
        val domains = presetManager.getAllBlockedDomains()
        // Atualiza a política do enforcer de DNS
        for (dom in domains) {
            dnsPolicy.addBlockedDomain(dom)
        }
    }

    fun startFocus(durationMs: Long) {
        syncPolicies()
        session.start(durationMs)
        _isFocusActive.value = true
    }

    fun startPomodoro(mission: String = "") {
        syncPolicies()
        pomodoroEngine.start(mission)
        session.start(pomodoroEngine.workDurationMs)
        _isFocusActive.value = true
    }

    fun checkStatus(): Boolean {
        val active = session.isActive() || pomodoroEngine.isBlockingActive()
        _isFocusActive.value = active
        return active
    }
}
