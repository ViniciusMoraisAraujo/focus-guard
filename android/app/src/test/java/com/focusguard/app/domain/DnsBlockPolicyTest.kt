package com.focusguard.app.domain

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test

class DnsBlockPolicyTest {

    private lateinit var policy: DnsBlockPolicy

    @Before
    fun setUp() {
        policy = DnsBlockPolicy(
            blockedDomains = setOf(
                "instagram.com",
                "tiktok.com",
                "twitter.com",
                "x.com"
            )
        )
    }

    @Test
    fun `does not sinkhole domain when session is inactive`() {
        assertFalse(
            "Domain should not be sinkholed when session is inactive",
            policy.shouldSinkhole("instagram.com", isSessionActive = false)
        )
    }

    @Test
    fun `sinkholes exact domain match when session is active`() {
        assertTrue(
            "Exact domain match should be sinkholed",
            policy.shouldSinkhole("instagram.com", isSessionActive = true)
        )
        assertTrue(
            "TikTok should be sinkholed",
            policy.shouldSinkhole("tiktok.com", isSessionActive = true)
        )
    }

    @Test
    fun `sinkholes subdomains of blocked domain`() {
        assertTrue(
            "www subdomain should be sinkholed",
            policy.shouldSinkhole("www.instagram.com", isSessionActive = true)
        )
        assertTrue(
            "api subdomain should be sinkholed",
            policy.shouldSinkhole("api.tiktok.com", isSessionActive = true)
        )
        assertTrue(
            "multi-level subdomain should be sinkholed",
            policy.shouldSinkhole("static.mobile.twitter.com", isSessionActive = true)
        )
    }

    @Test
    fun `does not sinkhole domains with partial name overlap`() {
        assertFalse(
            "notinstagram.com is a different domain",
            policy.shouldSinkhole("notinstagram.com", isSessionActive = true)
        )
        assertFalse(
            "tiktok.org is a different TLD",
            policy.shouldSinkhole("tiktok.org", isSessionActive = true)
        )
    }

    @Test
    fun `handles case-insensitivity and trailing dots`() {
        assertTrue(
            "Uppercase hostname should be normalized and sinkholed",
            policy.shouldSinkhole("WWW.INSTAGRAM.COM", isSessionActive = true)
        )
        assertTrue(
            "DNS standard trailing dot should be handled",
            policy.shouldSinkhole("instagram.com.", isSessionActive = true)
        )
    }

    @Test
    fun `allows adding and removing domains dynamically`() {
        policy.addBlockedDomain("reddit.com")
        assertTrue(policy.shouldSinkhole("reddit.com", isSessionActive = true))
        assertTrue(policy.shouldSinkhole("old.reddit.com", isSessionActive = true))

        policy.removeBlockedDomain("reddit.com")
        assertFalse(policy.shouldSinkhole("reddit.com", isSessionActive = true))
    }
}
