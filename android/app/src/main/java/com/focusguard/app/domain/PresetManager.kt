package com.focusguard.app.domain

import java.util.Locale

/**
 * PresetManager consolida o gerenciamento de categorias de bloqueio e
 * aplicativos customizados, aplicando a salvaguarda anti-burla contra
 * desativação de regras durante foco ativo.
 */
class PresetManager(
    private val isSessionActiveProvider: () -> Boolean = { false }
) {
    private val presetsMap = mutableMapOf<String, Preset>()
    private val customBlockedPackages = mutableSetOf<String>()

    init {
        loadDefaultPresets()
    }

    private fun loadDefaultPresets() {
        val social = Preset(
            id = "social",
            name = "Redes Sociais",
            description = "Instagram, TikTok, Twitter/X, Reddit, Facebook",
            apps = setOf(
                "com.instagram.android",
                "com.zhiliaoapp.musically", // TikTok
                "com.twitter.android",
                "com.x.android",
                "com.facebook.katana",
                "com.reddit.frontpage",
                "com.snapchat.android",
                "com.pinterest"
            ),
            domains = setOf(
                "instagram.com",
                "tiktok.com",
                "twitter.com",
                "x.com",
                "facebook.com",
                "reddit.com",
                "snapchat.com",
                "pinterest.com"
            ),
            isEnabled = true
        )

        val streaming = Preset(
            id = "streaming",
            name = "Vídeos & Streaming",
            description = "YouTube, Netflix, Disney+, Prime Video, Twitch",
            apps = setOf(
                "com.google.android.youtube",
                "com.netflix.mediaclient",
                "com.disney.disneyplus",
                "com.amazon.avod.thirdpartyclient",
                "tv.twitch.android.app"
            ),
            domains = setOf(
                "youtube.com",
                "netflix.com",
                "disneyplus.com",
                "primevideo.com",
                "twitch.tv"
            ),
            isEnabled = true
        )

        presetsMap[social.id] = social
        presetsMap[streaming.id] = streaming
    }

    fun getPresets(): List<Preset> = presetsMap.values.toList()

    fun setPresetEnabled(presetId: String, enabled: Boolean) {
        if (!enabled && isSessionActiveProvider()) {
            throw IllegalStateException(
                "Não é permitido desativar categorias de bloqueio durante uma sessão de foco ativa!"
            )
        }
        val current = presetsMap[presetId] ?: return
        presetsMap[presetId] = current.copy(isEnabled = enabled)
    }

    fun setCustomAppBlocked(packageName: String, blocked: Boolean) {
        if (!blocked && isSessionActiveProvider()) {
            throw IllegalStateException(
                "Não é permitido desbloquear aplicativos durante uma sessão de foco ativa!"
            )
        }
        if (blocked) {
            customBlockedPackages.add(packageName)
        } else {
            customBlockedPackages.remove(packageName)
        }
    }

    fun isCustomAppBlocked(packageName: String): Boolean {
        return customBlockedPackages.contains(packageName)
    }

    fun getAllBlockedPackages(): Set<String> {
        val result = mutableSetOf<String>()
        result.addAll(customBlockedPackages)
        for (preset in presetsMap.values) {
            if (preset.isEnabled) {
                result.addAll(preset.apps)
            }
        }
        return result
    }

    fun getAllBlockedDomains(): Set<String> {
        val result = mutableSetOf<String>()
        for (preset in presetsMap.values) {
            if (preset.isEnabled) {
                result.addAll(preset.domains)
            }
        }
        return result
    }

    fun isPackageBlocked(packageName: String): Boolean {
        if (customBlockedPackages.contains(packageName)) {
            return true
        }
        return presetsMap.values.any { it.isEnabled && it.apps.contains(packageName) }
    }

    fun isDomainBlocked(domain: String): Boolean {
        val clean = domain.trim().lowercase(Locale.ROOT)
        return presetsMap.values.any { preset ->
            preset.isEnabled && preset.domains.any { d ->
                clean == d || clean.endsWith(".$d")
            }
        }
    }
}
