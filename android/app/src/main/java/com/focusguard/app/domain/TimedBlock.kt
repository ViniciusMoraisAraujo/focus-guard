package com.focusguard.app.domain

enum class BlockTargetType {
    APP,
    PRESET
}

/**
 * TimedBlock representa um bloqueio temporizado independente no FocusGuard Android,
 * com suporte a contagem regressiva por hardware ticks e extensão anti-burla.
 */
data class TimedBlock(
    val id: String,
    val targetType: BlockTargetType,
    val targetValue: String,
    val displayName: String,
    var durationMs: Long,
    val startedAtElapsedMs: Long,
    var expiresAtElapsedMs: Long = startedAtElapsedMs + durationMs,
    var wallExpiresAtMs: Long = System.currentTimeMillis() + durationMs
) {
    init {
        require(durationMs > 0L) { "A duração do bloqueio deve ser maior que zero" }
    }

    /**
     * Retorna se o bloqueio está atualmente em vigor.
     */
    fun isActive(nowElapsedMs: Long): Boolean {
        return nowElapsedMs < expiresAtElapsedMs
    }

    /**
     * Retorna o tempo restante em milissegundos.
     */
    fun remainingTimeMs(nowElapsedMs: Long): Long {
        return if (isActive(nowElapsedMs)) {
            expiresAtElapsedMs - nowElapsedMs
        } else {
            0L
        }
    }

    /**
     * Estende o tempo de bloqueio adicionando milissegundos extras ao prazo de expiração.
     * Regra anti-burla: apenas incrementos positivos são aceitos.
     */
    fun extend(extraMs: Long) {
        if (extraMs <= 0L) return
        durationMs += extraMs
        expiresAtElapsedMs += extraMs
        wallExpiresAtMs += extraMs
    }
}
