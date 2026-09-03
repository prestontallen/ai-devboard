package store

import (
	"crypto/rand"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
)

var (
	entropyMu sync.Mutex
	entropy   = ulid.Monotonic(rand.Reader, 0)
)

// NewID mints a ULID. Minting happens exactly once per entity: the
// converter resolves by slug first and reuses the stored ID on every
// re-run (D4 — deterministic across rehearsals because the output store
// IS the persisted id-map).
func NewID() ID {
	entropyMu.Lock()
	defer entropyMu.Unlock()
	return ID(ulid.MustNew(ulid.Timestamp(time.Now()), entropy).String())
}

// NormalizeSlug lowercases an alias the way today's parser does
// (parse/work.go lowercases every ID on read).
func NormalizeSlug(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
