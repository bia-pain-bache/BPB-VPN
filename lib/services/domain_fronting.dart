/// Modified patterniha's MITM-DomainFronting Xray config, as published
/// (https://github.com/patterniha/MITM-DomainFronting, Xray-config/MITM-DomainFronting.json,
/// GPL-3.0), adapted for the app's exit routing, DNS, certificate files and traffic stats.
/// XrayConfigBuilder supplies the app's inbounds and, when selected, the exit.
const domainFronting = r'''
{
  "log": {
    "loglevel": "warning",
    "dnsLog": false
  },
  "dns": {
    "hosts": {
      "dns.redirect": [
        "1.1.1.1",
        "1.0.0.1"
      ]
    },
    "servers": [
      {
        "tag": "fake-dns",
        "address": "fakedns",
        "domains": [
          "full:github.githubassets.com"
        ]
      },
      {
        "tag": "no-filter-dns",
        "address": "h2c://1.1.1.1/dns-query",
        "finalQuery": true
      },
      {
        "tag": "local-dns",
        "address": "localhost",
        "domains": [
          "full:github.githubassets.com"
        ],
        "finalQuery": true
      }
    ],
    "queryStrategy": "UseSystem",
    "useSystemHosts": true,
    "serveStale": true
  },
  "inbounds": [
    {
      "tag": "mixed-in",
      "port": 10808,
      "protocol": "mixed",
      "sniffing": {
        "enabled": true,
        "destOverride": [
          "fakedns",
          "tls"
        ],
        "routeOnly": false
      },
      "settings": {
        "udp": true,
        "ip": "127.0.0.1"
      },
      "streamSettings": {
        "sockopt": {
          "tcpKeepAliveInterval": 1,
          "tcpKeepAliveIdle": 11
        }
      }
    },
    {
      "port": 11666,
      "tag": "tls-decrypt-h11",
      "protocol": "tunnel",
      "settings": {
        "network": "tcp",
        "port": 443,
        "followRedirect": true
      },
      "streamSettings": {
        "security": "tls",
        "tlsSettings": {
          "alpn": [
            "http/1.1"
          ],
          "certificates": [
            {
              "usage": "issue",
              "certificateFile": "bpb-cert.crt",
              "keyFile": "bpb-cert.key"
            }
          ]
        }
      }
    },
    {
      "port": 11777,
      "tag": "tls-decrypt-h211",
      "protocol": "tunnel",
      "settings": {
        "network": "tcp",
        "port": 443,
        "followRedirect": true
      },
      "streamSettings": {
        "security": "tls",
        "tlsSettings": {
          "alpn": [
            "h2",
            "http/1.1"
          ],
          "certificates": [
            {
              "usage": "issue",
              "certificateFile": "bpb-cert.crt",
              "keyFile": "bpb-cert.key"
            }
          ]
        }
      }
    }
  ],
  "outbounds": [
    {
      "tag": "block",
      "protocol": "block"
    },
    {
      "tag": "direct",
      "protocol": "direct",
      "streamSettings": {
        "sockopt": {
          "domainStrategy": "ForceIP",
          "happyEyeballs": {
            "tryDelayMs": 300,
            "prioritizeIPv6": false,
            "interleave": 2,
            "maxConcurrentTry": 20
          }
        }
      }
    },
    {
      "tag": "redirect-out-h11",
      "protocol": "direct",
      "settings": {
        "redirect": "127.0.0.1:11666"
      }
    },
    {
      "tag": "redirect-out-h211",
      "protocol": "direct",
      "settings": {
        "redirect": "127.0.0.1:11777"
      }
    },
    {
      "tag": "tls-repack-dns",
      "protocol": "direct",
      "settings": {
        "redirect": "dns.redirect:443"
      },
      "streamSettings": {
        "security": "tls",
        "tlsSettings": {
          "serverName": "www.google.com",
          "verifyPeerCertByName": "one.one.one.one",
          "alpn": [
            "h2"
          ],
          "fingerprint": "chrome"
        },
        "sockopt": {
          "domainStrategy": "ForceIP",
          "happyEyeballs": {
            "tryDelayMs": 300,
            "prioritizeIPv6": false,
            "interleave": 2,
            "maxConcurrentTry": 20
          }
        }
      }
    },
    {
      "tag": "tls-repack-google",
      "protocol": "direct",
      "streamSettings": {
        "security": "tls",
        "tlsSettings": {
          "serverName": "www.google.com",
          "verifyPeerCertByName": "fromMitM,www.google.com",
          "alpn": [
            "fromMitM"
          ],
          "fingerprint": "chrome"
        },
        "sockopt": {
          "domainStrategy": "ForceIP",
          "happyEyeballs": {
            "tryDelayMs": 300,
            "prioritizeIPv6": false,
            "interleave": 2,
            "maxConcurrentTry": 20
          }
        }
      }
    },
    {
      "tag": "tls-repack-fastly",
      "protocol": "direct",
      "settings": {
        "redirect": "github.githubassets.com:443"
      },
      "streamSettings": {
        "security": "tls",
        "tlsSettings": {
          "serverName": "github.githubassets.com",
          "verifyPeerCertByName": "fromMitM,github.githubassets.com",
          "alpn": [
            "fromMitM"
          ],
          "fingerprint": "chrome"
        },
        "sockopt": {
          "domainStrategy": "ForceIP",
          "happyEyeballs": {
            "tryDelayMs": 300,
            "prioritizeIPv6": false,
            "interleave": 2,
            "maxConcurrentTry": 20
          }
        }
      }
    },
    {
      "tag": "tls-repack-meta",
      "protocol": "direct",
      "streamSettings": {
        "security": "tls",
        "tlsSettings": {
          "serverName": "www.microsoft.com",
          "verifyPeerCertByName": "fromMitM,www.microsoft.com",
          "alpn": [
            "fromMitM"
          ],
          "fingerprint": "chrome"
        },
        "sockopt": {
          "domainStrategy": "ForceIP",
          "happyEyeballs": {
            "tryDelayMs": 300,
            "prioritizeIPv6": false,
            "interleave": 2,
            "maxConcurrentTry": 20
          }
        }
      }
    }
  ],
  "routing": {
    "domainStrategy": "IPOnDemand",
    "rules": [
      {
        "inboundTag": [],
        "port": 53,
        "outboundTag": "dns-out"
      },
      {
        "inboundTag": [
          "no-filter-dns"
        ],
        "outboundTag": "tls-repack-dns"
      },
      {
        "domain": [],
        "outboundTag": "block",
        "ruleTag": "block-domain"
      },
      {
        "domain": [],
        "outboundTag": "direct",
        "ruleTag": "bypass-domain"
      },
      {
        "inboundTag": [
          "tls-decrypt-h11"
        ],
        "domain": [
          "domain:googlevideo.com"
        ],
        "outboundTag": "tls-repack-google"
      },
      {
        "inboundTag": [
          "tls-decrypt-h11"
        ],
        "outboundTag": "block"
      },
      {
        "inboundTag": [
          "tls-decrypt-h211"
        ],
        "domain": [
          "geosite:google"
        ],
        "outboundTag": "tls-repack-google"
      },
      {
        "inboundTag": [
          "tls-decrypt-h211"
        ],
        "domain": [
          "geosite:fastly",
          "geosite:reddit",
          "geosite:cnn",
          "domain:buzzfeed.com"
        ],
        "outboundTag": "tls-repack-fastly"
      },
      {
        "inboundTag": [
          "tls-decrypt-h211"
        ],
        "domain": [
          "geosite:meta"
        ],
        "outboundTag": "tls-repack-meta"
      },
      {
        "inboundTag": [
          "tls-decrypt-h211"
        ],
        "ip": [
          "geoip:fastly"
        ],
        "outboundTag": "tls-repack-fastly"
      },
      {
        "inboundTag": [
          "tls-decrypt-h211"
        ],
        "outboundTag": "block"
      },
      {
        "inboundTag": [],
        "network": "tcp",
        "port": 443,
        "domain": [
          "domain:googlevideo.com"
        ],
        "outboundTag": "redirect-out-h11"
      },
      {
        "inboundTag": [],
        "network": "tcp",
        "port": 443,
        "domain": [
          "geosite:google",
          "geosite:meta",
          "geosite:fastly",
          "geosite:reddit",
          "geosite:cnn",
          "domain:buzzfeed.com"
        ],
        "outboundTag": "redirect-out-h211"
      },
      {
        "inboundTag": [],
        "network": "tcp",
        "port": 443,
        "ip": [
          "geoip:fastly"
        ],
        "outboundTag": "redirect-out-h211"
      },
      {
        "inboundTag": [],
        "ip": [],
        "outboundTag": "block",
        "ruleTag": "block-ip"
      },
      {
        "ip": [],
        "outboundTag": "direct",
        "ruleTag": "bypass-ip"
      },
      {
        "inboundTag": [],
        "ip": [
          "0.0.0.0/0",
          "::/0"
        ],
        "outboundTag": "direct"
      },
      {
        "inboundTag": [],
        "port": "0-65535",
        "outboundTag": "block"
      }
    ]
  },
  "policy": {
    "levels": {
      "0": {
        "uplinkOnly": 0,
        "downlinkOnly": 0
      }
    },
    "system": {
      "statsOutboundUplink": true,
      "statsOutboundDownlink": true
    }
  },
  "stats": {}
}
''';
