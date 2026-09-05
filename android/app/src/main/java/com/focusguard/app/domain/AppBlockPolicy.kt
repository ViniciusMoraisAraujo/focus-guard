package com.focusguard.app.domain

/**
 * AppBlockPolicy avalia determinísticamente se um aplicativo Android
 * deve ser bloqueado com base na sessão de foco e na lista de restrições.
 */
class AppBlockPolicy(
    blockedPackages: Set<String> = emptySet()
) {
    private val blocked: MutableSet<String> = blockedPackages.toMutableSet()

    private val essentialWhitelist: Set<String> = setOf(
        "com.focusguard.app",
        "com.android.systemui",
        "com.google.android.dialer",
        "com.android.dialer",
        "com.samsung.android.dialer",
        "com.android.phone",
        "com.android.emergency"
    )

    /**
     * Avalia se um pacote em primeiro plano deve ser interceptado e bloqueado.
     */
    fun shouldBlock(packageName: String, isSessionActive: Boolean): Boolean {
        if (!isSessionActive) {
            return false
        }
        if (essentialWhitelist.contains(packageName)) {
            return false
        }
        return blocked.contains(packageName)
    }

    /**
     * Detecta se o usuário está tentando acessar telas de desinstalação ou
     * forçar parada do FocusGuard nas configurações do sistema enquanto
     * uma sessão de foco está em andamento.
     */
    fun isSettingsTamper(
        packageName: String,
        className: String,
        windowText: List<String>,
        isSessionActive: Boolean
    ): Boolean {
        if (!isSessionActive) {
            return false
        }
        if (packageName != "com.android.settings") {
            return false
        }

        // Verifica se a tela de detalhes de app contém menção ao FocusGuard
        val mentionsFocusGuard = windowText.any { it.contains("FocusGuard", ignoreCase = true) }
        val mentionsUninstallOrStop = windowText.any {
            it.contains("Desinstalar", ignoreCase = true) ||
            it.contains("Uninstall", ignoreCase = true) ||
            it.contains("Forçar parada", ignoreCase = true) ||
            it.contains("Force stop", ignoreCase = true)
        }

        return mentionsFocusGuard && mentionsUninstallOrStop
    }

    fun addBlockedPackage(packageName: String) {
        if (!essentialWhitelist.contains(packageName)) {
            blocked.add(packageName)
        }
    }

    fun removeBlockedPackage(packageName: String) {
        blocked.remove(packageName)
    }

    fun getBlockedPackages(): Set<String> = blocked.toSet()
}
