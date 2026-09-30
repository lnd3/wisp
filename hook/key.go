package hook

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/netip"
)

// saltSize is the daily salt length in bytes.
const saltSize = 32

// keySize is how many bytes of the HMAC output form a visitor key —
// 128 bits is ample to avoid collisions within one product-day.
const keySize = 16

type salt [saltSize]byte

func newSalt() (salt, error) {
	var s salt
	_, err := rand.Read(s[:])
	return s, err
}

// wipe zeroes the salt before it's dropped. Best effort (the GC may have
// copies), but it keeps a discarded salt out of any later memory dump of
// this struct.
func (s *salt) wipe() {
	for i := range s {
		s[i] = 0
	}
}

// normalizeIP returns the bytes the visitor key is derived from: an IPv4
// address as its 4 bytes (IPv4-mapped IPv6 is unmapped first), an IPv6
// address truncated to its /64 prefix — privacy extensions rotate the low
// 64 bits, and a /64 is roughly one household or LAN, which fits the
// deliberate lower bound.
func normalizeIP(raw string) ([]byte, bool) {
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return nil, false
	}
	addr = addr.Unmap().WithZone("")
	if addr.Is4() {
		b := addr.As4()
		return b[:], true
	}
	b := addr.As16()
	return b[:8], true
}

// isPrivate reports loopback, private and link-local addresses — what a
// product sees as the "client" when it sits behind a proxy and hasn't
// configured Config.ClientIP.
func isPrivate(raw string) bool {
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return false
	}
	addr = addr.Unmap()
	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast()
}

// visitorKey is base64url(HMAC-SHA256(salt, ipnorm ‖ 0x1F ‖ ua)[:16]).
// Never a plain hash: IPv4 × real User-Agents is small enough to
// brute-force, so the key is only as safe as the salt's secrecy.
func visitorKey(s *salt, ipnorm []byte, ua string) string {
	m := hmac.New(sha256.New, s[:])
	m.Write(ipnorm)
	m.Write([]byte{0x1F})
	m.Write([]byte(ua))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil)[:keySize])
}
