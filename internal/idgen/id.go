package idgen

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"time"
)

// New returns a compact random identifier suitable for request/event/message IDs.
func New() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return hex.EncodeToString(b[:])
}
