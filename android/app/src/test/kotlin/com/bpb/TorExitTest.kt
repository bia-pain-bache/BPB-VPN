package com.bpb

import org.junit.Assert.assertEquals
import org.junit.Test

class TorExitTest {
    @Test
    fun `Snowflake AMP bridge swaps the rendezvous for the AMP cache`() {
        assertEquals(
            "snowflake 192.0.2.3:80 FP fingerprint=FP ice=stun:a.example:3478 utls-imitate=hello " +
                "url=https://snowflake-broker.torproject.net/ " +
                "ampcache=https://cdn.ampproject.org/ front=www.google.com",
            TorExit.snowflakeAmpBridge(
                "snowflake 192.0.2.3:80 FP fingerprint=FP " +
                    "url=https://broker.example/ fronts=front.example " +
                    "ice=stun:a.example:3478 utls-imitate=hello",
            ),
        )
    }
}
