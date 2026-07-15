package netutil

import "net"

// isDocumentationIP checks for TEST-NET ranges (192.0.2.0/24, 198.51.100.0/24,
// 203.0.113.0/24) and documentation IPv6 (2001:db8::/32).
func isDocumentationIP(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		// 192.0.2.0/24
		if ip4[0] == 192 && ip4[1] == 0 && ip4[2] == 2 {
			return true
		}
		// 198.51.100.0/24
		if ip4[0] == 198 && ip4[1] == 51 && ip4[2] == 100 {
			return true
		}
		// 203.0.113.0/24
		if ip4[0] == 203 && ip4[1] == 0 && ip4[2] == 113 {
			return true
		}
		return false
	}
	// 2001:db8::/32
	return len(ip) == net.IPv6len && ip[0] == 0x20 && ip[1] == 0x01 && ip[2] == 0x0d && ip[3] == 0xb8
}

// isReservedIP checks for IANA reserved ranges that should not be targeted.
func isReservedIP(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		// 240.0.0.0/4 (future use / reserved)
		if ip4[0] >= 240 {
			return true
		}
		return false
	}
	// IPv6 reserved ranges: 100::/64 (discard), fec0::/10 (site-local, deprecated)
	if len(ip) == net.IPv6len && ip[0] == 0x01 && ip[1] == 0x00 {
		return true
	}
	if len(ip) == net.IPv6len && ip[0] == 0xfe && (ip[1]&0xc0) == 0xc0 {
		return true
	}
	return false
}
