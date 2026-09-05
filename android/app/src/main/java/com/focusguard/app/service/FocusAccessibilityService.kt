package com.focusguard.app.service

import android.accessibilityservice.AccessibilityService
import android.content.Intent
import android.view.accessibility.AccessibilityEvent
import android.view.accessibility.AccessibilityNodeInfo
import com.focusguard.app.domain.FocusGuardManager

/**
 * FocusAccessibilityService monitora eventos de janela do sistema operacional
 * para identificar a abertura de aplicativos bloqueados ou tentativas de burla.
 */
class FocusAccessibilityService : AccessibilityService() {

    override fun onAccessibilityEvent(event: AccessibilityEvent?) {
        if (event == null || event.eventType != AccessibilityEvent.TYPE_WINDOW_STATE_CHANGED) {
            return
        }

        val packageName = event.packageName?.toString() ?: return
        val className = event.className?.toString() ?: ""
        val isSessionActive = FocusGuardManager.checkStatus()

        // 1. Verificação de aplicativo bloqueado
        if (FocusGuardManager.appPolicy.shouldBlock(packageName, isSessionActive)) {
            handleBlockedAppTrigger(packageName)
            return
        }

        // 2. Verificação de tentativa de burla nas Configurações do Android
        if (isSessionActive && packageName == "com.android.settings") {
            val texts = extractWindowTexts(rootInActiveWindow)
            if (FocusGuardManager.appPolicy.isSettingsTamper(packageName, className, texts, isSessionActive)) {
                // Impede a ação de desinstalar/forçar parada retornando o usuário à tela inicial
                performGlobalAction(GLOBAL_ACTION_HOME)
            }
        }
    }

    private fun handleBlockedAppTrigger(packageName: String) {
        // Redireciona o usuário para a Home para fechar a visualização do app bloqueado
        performGlobalAction(GLOBAL_ACTION_HOME)

        // Exibe a tela de Interceptor Overlay com exercício de respiração
        com.focusguard.app.ui.interceptor.InterceptorActivity.launch(this, packageName)
    }

    private fun extractWindowTexts(node: AccessibilityNodeInfo?): List<String> {
        val texts = mutableListOf<String>()
        if (node == null) return texts

        fun traverse(n: AccessibilityNodeInfo) {
            n.text?.toString()?.let { texts.add(it) }
            n.contentDescription?.toString()?.let { texts.add(it) }
            for (i in 0 until n.childCount) {
                n.getChild(i)?.let { traverse(it) }
            }
        }

        traverse(node)
        return texts
    }

    override fun onInterrupt() {
        // Callback obrigatório quando o serviço é interrompido pelo sistema
    }

    companion object {
        const val ACTION_SHOW_OVERLAY = "com.focusguard.app.action.SHOW_OVERLAY"
        const val EXTRA_BLOCKED_PACKAGE = "extra_blocked_package"
    }
}
