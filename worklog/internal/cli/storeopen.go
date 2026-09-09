package cli

import (
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/memstore"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/sqlitestore"
)

// This file is the ONLY place production code constructs a store.
//
// The epic's claim is that a second adapter is a drop-in. That was not true
// while five call sites across four files each named an implementation:
// swapping meant editing all five. It is true when it means editing this file.
//
// The guard for that is in internal/store — and it counts CONSTRUCTION rather
// than banning imports, because every site lives in internal/cli, the
// composition root, so a directory-scoped import ban would leave all of them
// legal and prove nothing.
//
// One site is deliberately not here: `store relocate` opens a file it just
// copied, to prove the copy is a real database rather than bytes of the right
// length, and deletes it if not. That is a file-copy verification. A second
// adapter that is not file-backed has nothing for it to do, so routing it
// through here would be a false uniformity. The guard names it as a permanent
// exception with that reason.

// storeMode says what the caller needs, in terms of intent rather than of a
// named implementation.
//
// The distinction is load-bearing for adoption: a dry run must leave NO trace
// on a machine that has no store, including not creating the database, and a
// commit exists precisely to create it. Expressing that as "give me the
// in-memory one" would re-export the implementation choice to the caller,
// which is the thing this file exists to stop.
type storeMode int

const (
	// storeExisting opens a store that is already there. It does not promise
	// to refuse a missing one — sqlitestore.Open CREATES on a missing path,
	// which is why requireStore stats first and stays in the caller.
	storeExisting storeMode = iota
	// storeCreate opens, creating if absent. Adoption's commit path.
	storeCreate
	// storeEphemeral is a throwaway store that touches no disk. A preview
	// that must leave nothing behind.
	storeEphemeral
)

// openStore is the single construction path.
//
// It returns a close func rather than relying on the caller to type-assert
// something closable, so an ephemeral store and a durable one are the same
// shape at every call site.
func openStore(mode storeMode, path string) (store.Store, func(), error) {
	if mode == storeEphemeral {
		return memstore.New(), func() {}, nil
	}
	s, err := sqlitestore.Open(path)
	if err != nil {
		return nil, func() {}, err
	}
	return s, func() { s.Close() }, nil
}
