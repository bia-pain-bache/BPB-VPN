package com.bpb

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class PhysicalNetworksTest {
    @Test
    fun `underlying network arrival and loss trigger checks even when another network remains`() {
        val networks = PhysicalNetworks(mapOf("wifi" to "wifi-link"))
        assertFalse(networks.available("wifi"))
        assertFalse(networks.linkChanged("wifi", "wifi-link"))
        assertTrue(networks.available("mobile"))
        assertFalse(networks.linkChanged("mobile", "mobile-link"))
        assertTrue(networks.lost("wifi"))
        assertFalse(networks.lost("wifi"))
    }

    @Test
    fun `only real changes to known links trigger checks`() {
        val networks = PhysicalNetworks(mapOf("wifi" to "address-1"))
        assertFalse(networks.linkChanged("unknown", "link"))
        assertTrue(networks.linkChanged("wifi", "address-2"))
        assertFalse(networks.linkChanged("wifi", "address-2"))
        assertTrue(networks.lost("wifi"))
        assertFalse(networks.linkChanged("wifi", "address-3"))
    }
}
