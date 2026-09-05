package com.focusguard.app.domain

import java.util.Locale

/**
 * DnsBlockPolicy avalia se uma requisição DNS para determinado domínio
 * deve ser redirecionada para o sinkhole local (0.0.0.0).
 */
class DnsBlockPolicy(
    blockedDomains: Set<String> = emptySet()
) {
    private val blocked: MutableSet<String> = blockedDomains.map { sanitize(it) }.toMutableSet()

    /**
     * Avalia se um hostname consultado deve receber resposta sinkhole.
     */
    fun shouldSinkhole(hostname: String, isSessionActive: Boolean): Boolean {
        if (!isSessionActive) {
            return false
        }
        val cleanHost = sanitize(hostname)
        if (cleanHost.isEmpty()) {
            return false
        }

        // 1. Correspondência exata (ex: instagram.com)
        if (blocked.contains(cleanHost)) {
            return true
        }

        // 2. Correspondência de subdomínio (ex: api.instagram.com termina com .instagram.com)
        return blocked.any { blockedDomain ->
            cleanHost.endsWith(".$blockedDomain")
        }
    }

    fun addBlockedDomain(domain: String) {
        val clean = sanitize(domain)
        if (clean.isNotEmpty()) {
            blocked.add(clean)
        }
    }

    fun removeBlockedDomain(domain: String) {
        blocked.remove(sanitize(domain))
    }

    fun getBlockedDomains(): Set<String> = blocked.toSet()

    private fun sanitize(input: String): String {
        var domain = input.trim().lowercase(Locale.ROOT)
        // Remove trailing dot padrão de FQDN DNS (ex: instagram.com.)
        if (domain.endsWith(".")) {
            domain = domain.substring(0, domain.length - 1)
        }
        // Remove esquemas http/https se presentes
        if (domain.startsWith("http://")) {
            domain = domain.removePrefix("http://")
        } else if (domain.startsWith("https://")) {
            domain = domain.removePrefix("https://")
        }
        // Remove barras ou paths acidentais
        val slashIdx = domain.indexOf('/')
        if (slashIdx != -1) {
            domain = domain.substring(0, slashIdx)
        }
        // Remove portas acidentais (ex: domain:80)
        val colonIdx = domain.indexOf(':')
        if (colonIdx != -1) {
            domain = domain.substring(0, colonIdx)
        }
        return domain
    }
}
