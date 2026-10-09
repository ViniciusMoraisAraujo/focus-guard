package com.focusguard.app.domain

import android.os.SystemClock
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

/**
 * TimedBlockManager gerencia bloqueios de aplicativos e categorias por duração
 * determinada, garantindo conformidade com a política anti-burla do FocusGuard.
 */
class TimedBlockManager(
    private val clock: () -> Long = { SystemClock.elapsedRealtime() }
) {
    private val blocksMap = mutableMapOf<String, TimedBlock>()

    private val _blocksFlow = MutableStateFlow<List<TimedBlock>>(emptyList())
    val blocksFlow: StateFlow<List<TimedBlock>> = _blocksFlow.asStateFlow()

    private fun notifyChanges() {
        _blocksFlow.value = getActiveBlocks()
    }

    /**
     * Bloqueia um aplicativo específico por uma duração determinada em milissegundos.
     */
    fun blockApp(packageName: String, displayName: String, durationMs: Long): TimedBlock {
        val now = clock()
        val existing = blocksMap[packageName]
        val block = if (existing != null && existing.isActive(now)) {
            existing.extend(durationMs)
            existing
        } else {
            TimedBlock(
                id = packageName,
                targetType = BlockTargetType.APP,
                targetValue = packageName,
                displayName = displayName,
                durationMs = durationMs,
                startedAtElapsedMs = now
            )
        }
        blocksMap[packageName] = block
        notifyChanges()
        return block
    }

    /**
     * Bloqueia uma categoria/preset inteira por uma duração determinada.
     */
    fun blockPreset(presetId: String, displayName: String, durationMs: Long): TimedBlock {
        val now = clock()
        val existing = blocksMap[presetId]
        val block = if (existing != null && existing.isActive(now)) {
            existing.extend(durationMs)
            existing
        } else {
            TimedBlock(
                id = presetId,
                targetType = BlockTargetType.PRESET,
                targetValue = presetId,
                displayName = displayName,
                durationMs = durationMs,
                startedAtElapsedMs = now
            )
        }
        blocksMap[presetId] = block
        notifyChanges()
        return block
    }

    /**
     * Estende o tempo de um bloqueio ativo existente.
     */
    fun extendBlock(id: String, extraMs: Long): TimedBlock? {
        val now = clock()
        val block = blocksMap[id] ?: return null
        if (block.isActive(now)) {
            block.extend(extraMs)
            notifyChanges()
            return block
        }
        return null
    }

    /**
     * Cancela um bloqueio expirado.
     * Regra anti-burla: lança exceção se o bloqueio ainda estiver ativo.
     */
    fun cancelBlock(id: String) {
        val now = clock()
        val block = blocksMap[id] ?: return
        if (block.isActive(now)) {
            throw IllegalStateException("Não é permitido desbloquear antes do término do tempo (anti-burla)!")
        }
        blocksMap.remove(id)
        notifyChanges()
    }

    /**
     * Verifica se o pacote está sujeito a um bloqueio temporizado ativo (direto ou por preset).
     */
    fun isPackageBlocked(packageName: String, presetManager: PresetManager): Boolean {
        val now = clock()

        // 1. Bloqueio direto do app
        val direct = blocksMap[packageName]
        if (direct != null && direct.isActive(now)) {
            return true
        }

        // 2. Bloqueio via preset de categoria
        for (preset in presetManager.getPresets()) {
            val presetBlock = blocksMap[preset.id]
            if (presetBlock != null && presetBlock.isActive(now)) {
                if (preset.apps.contains(packageName)) {
                    return true
                }
            }
        }

        return false
    }

    /**
     * Retorna o tempo restante de bloqueio para um pacote específico em milissegundos.
     */
    fun getRemainingTimeForPackage(packageName: String, presetManager: PresetManager): Long {
        val now = clock()
        var maxRemaining = 0L

        val direct = blocksMap[packageName]
        if (direct != null && direct.isActive(now)) {
            maxRemaining = maxOf(maxRemaining, direct.remainingTimeMs(now))
        }

        for (preset in presetManager.getPresets()) {
            val presetBlock = blocksMap[preset.id]
            if (presetBlock != null && presetBlock.isActive(now) && preset.apps.contains(packageName)) {
                maxRemaining = maxOf(maxRemaining, presetBlock.remainingTimeMs(now))
            }
        }

        return maxRemaining
    }

    /**
     * Retorna todos os bloqueios que estão atualmente ativos.
     */
    fun getActiveBlocks(): List<TimedBlock> {
        val now = clock()
        return blocksMap.values.filter { it.isActive(now) }
    }

    /**
     * Retorna se há pelo menos um bloqueio temporizado ativo.
     */
    fun hasActiveBlocks(): Boolean {
        val now = clock()
        return blocksMap.values.any { it.isActive(now) }
    }

    /**
     * Retorna o tempo restante máximo entre todos os bloqueios ativos.
     */
    fun maxRemainingTimeMs(): Long {
        val now = clock()
        return blocksMap.values
            .filter { it.isActive(now) }
            .maxOfOrNull { it.remainingTimeMs(now) } ?: 0L
    }
}
