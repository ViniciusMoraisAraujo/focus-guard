package com.focusguard.app.domain

import android.os.SystemClock

/**
 * FocusSession gerencia o ciclo de vida e estado de uma sessão de foco no FocusGuard Android.
 *
 * Utiliza hardware ticks (SystemClock.elapsedRealtime por padrão) para prevenir
 * fraudes causadas por adiantamento manual do relógio do sistema.
 */
class FocusSession(
    private val clock: () -> Long = { SystemClock.elapsedRealtime() }
) {
    private var expiresAtMs: Long = 0L

    /**
     * Retorna se uma sessão de foco está atualmente em andamento.
     */
    fun isActive(): Boolean {
        return remainingTimeMs() > 0L
    }

    /**
     * Inicia uma nova sessão de foco com a duração especificada em milissegundos.
     */
    fun start(durationMs: Long) {
        require(durationMs > 0L) { "A duração do foco deve ser maior que zero" }
        expiresAtMs = clock() + durationMs
    }

    /**
     * Retorna o tempo restante de foco em milissegundos.
     */
    fun remainingTimeMs(): Long {
        val now = clock()
        return if (expiresAtMs > now) {
            expiresAtMs - now
        } else {
            0L
        }
    }

    /**
     * Regra anti-burla: o usuário só pode cancelar ou desinstalar o aplicativo
     * quando NÃO houver sessão de foco ativa.
     */
    fun canCancel(): Boolean {
        return !isActive()
    }

    /**
     * Encerra a sessão. Durante uma sessão ativa, lançará uma exceção para impedir
     * interrupções prematuras (anti-burla).
     */
    fun stop() {
        check(canCancel()) {
            "Não é permitido cancelar ou encerrar o aplicativo durante uma sessão de foco ativa!"
        }
        expiresAtMs = 0L
    }
}
