package com.focusguard.app.domain

/**
 * Preset representa uma categoria temática de bloqueio que agrupa
 * aplicativos móveis e domínios web correlacionados.
 */
data class Preset(
    val id: String,
    val name: String,
    val description: String,
    val apps: Set<String>,
    val domains: Set<String>,
    val isEnabled: Boolean = true
)
