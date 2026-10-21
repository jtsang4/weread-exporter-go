package export

import (
	"crypto/sha1" //nolint:gosec // RFC 4122 name-based UUIDs are defined in terms of SHA-1
	"fmt"
)

// namespaceURL is the RFC 4122 namespace for URLs.
var namespaceURL = [16]byte{0x6b, 0xa7, 0xb8, 0x11, 0x9d, 0xad, 0x11, 0xd1, 0x80, 0xb4, 0x00, 0xc0, 0x4f, 0xd4, 0x30, 0xc8}

// uuidV5 returns the name-based (SHA-1) UUID of name in the given namespace.
// EPUB readers key reading progress and annotations on the book identifier, so
// the same book must always get the same one.
func uuidV5(namespace [16]byte, name string) string {
	h := sha1.New() //nolint:gosec // see import comment
	h.Write(namespace[:])
	h.Write([]byte(name))
	sum := h.Sum(nil)

	var u [16]byte
	copy(u[:], sum[:16])
	u[6] = (u[6] & 0x0f) | 0x50 // version 5
	u[8] = (u[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
}
