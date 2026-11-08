package com.bpb

/** Serializes verification attempts, including shutdown from the service lifecycle. */
internal class VerificationState {
    sealed interface Result {
        data object Ignored : Result
        data object Connected : Result
        data class Retry(val delayMs: Long, val stale: Boolean = false) : Result
    }

    @Volatile var enabled = false
        private set
    @Volatile var generation = 0L
        private set
    @Volatile var inFlight = false
        private set
    private var retries = 0

    @Synchronized fun start() {
        enabled = true
    }

    @Synchronized fun request(): Long {
        generation++
        retries = 0
        return generation
    }

    @Synchronized fun begin(ticket: Long): Boolean {
        if (!enabled || inFlight || ticket != generation) return false
        inFlight = true
        return true
    }

    @Synchronized fun finish(ticket: Long, success: Boolean): Result {
        inFlight = false
        if (!enabled) return Result.Ignored
        if (ticket != generation) return Result.Retry(500, stale = true)
        if (success) return Result.Connected
        val seconds = minOf(2L shl minOf(retries++, 4), 30L)
        return Result.Retry(seconds * 1000)
    }

    @Synchronized fun stop() {
        enabled = false
        generation++
        retries = 0
    }
}
