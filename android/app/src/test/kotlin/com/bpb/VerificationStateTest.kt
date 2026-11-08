package com.bpb

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class VerificationStateTest {
    @Test
    fun `failure retries with capped backoff until success`() {
        val state = VerificationState()
        state.start()
        val ticket = state.request()
        for (delay in listOf(2000L, 4000L, 8000L, 16000L, 30000L, 30000L)) {
            assertTrue(state.begin(ticket))
            assertEquals(VerificationState.Result.Retry(delay), state.finish(ticket, false))
        }
        assertTrue(state.begin(ticket))
        assertEquals(VerificationState.Result.Connected, state.finish(ticket, true))
    }

    @Test
    fun `network change invalidates in-flight success and resets backoff`() {
        val state = VerificationState()
        state.start()
        val old = state.request()
        assertTrue(state.begin(old))
        assertEquals(VerificationState.Result.Retry(2000), state.finish(old, false))
        assertTrue(state.begin(old))
        val current = state.request()
        assertFalse(state.begin(current))
        assertEquals(VerificationState.Result.Retry(500, stale = true), state.finish(old, true))
        assertFalse(state.begin(old))
        assertTrue(state.begin(current))
        assertEquals(VerificationState.Result.Retry(2000), state.finish(current, false))
    }

    @Test
    fun `stop ignores outstanding results and restart does not accept stale success`() {
        val state = VerificationState()
        state.start()
        val old = state.request()
        assertTrue(state.begin(old))
        state.stop()
        assertEquals(VerificationState.Result.Ignored, state.finish(old, true))
        assertFalse(state.begin(old))
        state.start()
        val current = state.request()
        assertTrue(state.begin(current))
        assertEquals(VerificationState.Result.Connected, state.finish(current, true))
    }

    @Test
    fun `restart waits for an old in-flight probe`() {
        val state = VerificationState()
        state.start()
        val old = state.request()
        assertTrue(state.begin(old))
        state.stop()
        state.start()
        val current = state.request()
        assertFalse(state.begin(current))
        assertEquals(VerificationState.Result.Retry(500, stale = true), state.finish(old, true))
        assertTrue(state.begin(current))
        assertEquals(VerificationState.Result.Connected, state.finish(current, true))
    }
}
