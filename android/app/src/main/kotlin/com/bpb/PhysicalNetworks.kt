package com.bpb

/** Tracks the internet-capable non-VPN networks reported by ConnectivityManager. */
internal class PhysicalNetworks<N, L>(initial: Map<N, L?>) {
    private val networks = initial.toMutableMap()

    fun available(network: N): Boolean {
        if (networks.containsKey(network)) return false
        networks[network] = null
        return true
    }

    fun linkChanged(network: N, link: L): Boolean {
        if (!networks.containsKey(network)) return false
        val previous = networks.put(network, link)
        return previous != null && previous != link
    }

    fun lost(network: N): Boolean {
        if (!networks.containsKey(network)) return false
        networks.remove(network)
        return true
    }
}
