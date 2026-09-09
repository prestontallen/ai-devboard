package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/prestontallen/ai-devboard/worklog/internal/blockmap"
	"github.com/prestontallen/ai-devboard/worklog/internal/boardmap"
	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/projection"
	"github.com/prestontallen/ai-devboard/worklog/internal/serve"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
	"github.com/prestontallen/ai-devboard/worklog/internal/storepath"
	"github.com/prestontallen/ai-devboard/worklog/internal/yamlx"
)

// newServeCmd wires the devboard dashboard server. Configuration is
// env-driven (DEVBOARD_WORKLOG, DEVBOARD_PORT, DEVBOARD_SCAN_INTERVAL).
//
// DEVBOARD_DATA is NOT among them any more: the board is served from the
// store and there is no data directory to point at. It stayed in this help
// text after the code stopped reading it, which mattered once uninstall
// began offering to remove that directory — the tool would have been
// calling it retired in one breath and advertising it as live config in
// the next.
func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Args:  cobra.NoArgs,
		Short: "Serve the devboard dashboard (frontend + API, port 8484)",
		Long: `serve runs the devboard dashboard: the embedded frontend, the
/api/tasks feed grouped by repo directory, an SSE change stream, and the
archive/unarchive endpoints. It replaces devboard/server.py; the response
shape is frozen as the frontend contract (devboard/API.md).

Environment: DEVBOARD_WORKLOG (default ~/.local/share/worklog), DEVBOARD_PORT (8484),
DEVBOARD_SCAN_INTERVAL (seconds, 1.0). Binds 0.0.0.0 — the board is used
over LAN.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := serve.ConfigFromEnv()
			srv := serve.New(cfg)
			// Sync the store after a dashboard archive/unarchive move,
			// injected here rather than imported by internal/serve
			// directly. The original reason was an import cycle through
			// internal/verify, which is now deleted; the seam stays because
			// internal/projection's own tests import internal/serve, so a
			// serve→projection import would still cycle in the test binary.
			// Whoever flips serve to read the store directly has to deal
			// with that (adb-serve-store-direct).
			srv.MutateBoard = func(repo, id string, archived bool) (bool, error) {
				wd, err := model.NewWorkdir(cfg.WorklogDir)
				if err != nil {
					return false, err
				}
				return storeArchiveMove(wd, id, archived)
			}
			srv.LoadStoreSnapshot = loadStoreTickets
			srv.StoreFingerprint = newStoreFingerprint()
			return srv.ListenAndServe()
		},
	}
}

// loadStoreTickets is the store-direct read path: one open, every value
// the payload needs, nothing keyed on a rendered path.
//
// Stat before open, because sqlitestore.Open creates and migrates and a
// board read on an un-adopted machine would mint an empty db after which
// every CLI write refuses. Open and close within the call, so the WAL can
// checkpoint and `store relocate` is not refused forever. Read only: no
// stamps taken, no files written.
func loadStoreTickets() (*serve.StoreSnapshot, error) {
	wd, err := model.NewWorkdir(serve.ConfigFromEnv().WorklogDir)
	if err != nil {
		return nil, err
	}
	path := storepath.DB(wd.Root)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil // not adopted: serve an empty board, never create the db
	} else if err != nil {
		return nil, err
	}
	ss, _, err := openStore(storeExisting, path)
	if err != nil {
		return nil, err
	}
	defer ss.Close()
	return storeSnapshot(ss)
}

// storeSnapshot builds the payload's inputs from a store. Split from the
// opener so tests can drive it with a memstore.
func storeSnapshot(s store.Store) (*serve.StoreSnapshot, error) {
	tickets, err := s.Tickets()
	if err != nil {
		return nil, err
	}
	snap := &serve.StoreSnapshot{
		Notes:   map[string]string{},
		Backlog: map[model.SectionName][]model.Block{},
	}
	for _, t := range tickets {
		if t.Slug != "" && (t.NotesPreamble != "" || len(t.NoteEntries) > 0) {
			snap.Notes[t.Slug] = string(projection.NotesFile(t))
		}
		if !t.BoardTracked || t.ParentID != "" {
			continue // a child renders inside its epic's file, never its own
		}
		kids, err := s.Children(t.ID)
		if err != nil {
			return nil, err
		}
		// The board body goes through boardmap and an in-memory YAML round
		// trip rather than encoding the struct: devboard.Task carries yaml
		// tags only, with `,inline` extras at every level, so encoding/json
		// cannot reproduce the frozen shape. boardmap stays the single
		// store-to-board field correspondence either way.
		body, err := yamlx.YAMLToAny(boardmap.BoardYAML(t, kids))
		if err != nil {
			return nil, fmt.Errorf("rendering %s: %w", t.Slug, err)
		}
		task, ok := body.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("rendering %s: top level is not a mapping", t.Slug)
		}
		// Repo attribution heals here the way the renderer's grouping does:
		// the ticket's canonical repo decides the group, and an
		// unattributed ticket groups under "unknown".
		repo := t.Repo
		if repo == "" {
			repo = "unknown"
		}
		snap.Tasks = append(snap.Tasks, serve.StoreTask{
			Repo: repo, Slug: t.Slug, Archived: t.BoardArchived,
			MTime: t.UpdatedAt, Task: task,
		})
	}
	snap.Backlog[model.SectionNext] = blockmap.Blocks(tickets, store.SectionNext)
	snap.Backlog[model.SectionSomeday] = blockmap.Blocks(tickets, store.SectionSomeday)
	fb, err := s.Feedback()
	if err != nil {
		return nil, err
	}
	snap.Feedback = projection.FeedbackMD(fb)
	return snap, nil
}

// newStoreFingerprint returns the change watcher's signal: has anything
// the board draws changed. It runs every scan interval forever, so cost is
// the design, and each call keeps its own cache rather than a package
// global so two servers (and two tests) never share one.
//
// Two stages. The gate is a stat of the database and its WAL sidecars,
// which is free and cannot miss a write: SQLite rewrites rows even when
// the values are identical, so any write moves the files. That is also why
// the gate cannot be the answer on its own — a `task phase` setting the
// phase it already had would fire an event, and the file watcher this
// replaces did not, because an identical render was skipped and the
// mtime never moved.
//
// So when the gate moves, the snapshot is read and hashed, and only a
// changed hash is a change. Measured on the live store that read is 80ms
// against a stat's nothing: affordable once per write, not once per
// second.
func newStoreFingerprint() func() (string, error) {
	return newStoreFingerprintWith(loadStoreTickets)
}

// newStoreFingerprintWith is newStoreFingerprint with the read injected, so
// a test can wedge a write into the middle of one and prove the caching
// order. The window is real but far too narrow to hit by racing.
func newStoreFingerprintWith(load func() (*serve.StoreSnapshot, error)) func() (string, error) {
	var (
		mu       sync.Mutex
		lastGate string
		lastHash string
	)
	return func() (string, error) {
		wd, err := model.NewWorkdir(serve.ConfigFromEnv().WorklogDir)
		if err != nil {
			return "", err
		}
		path := storepath.DB(wd.Root)
		gate := dbFilesSig(path)

		mu.Lock()
		cached, ok := lastHash, gate == lastGate && lastHash != ""
		mu.Unlock()
		if ok {
			return cached, nil
		}

		snap, err := load()
		if err != nil {
			return "", err
		}
		sum := sha256.New()
		if snap != nil {
			if err := json.NewEncoder(sum).Encode(snap); err != nil {
				return "", err
			}
		}
		hash := hex.EncodeToString(sum.Sum(nil))

		mu.Lock()
		// The gate cached is the one sampled BEFORE the read, deliberately.
		// A write landing mid-read may not be in this snapshot, and that
		// write has already moved the files — so the next poll sees a gate
		// that differs from this one and reads again. Re-stating here would
		// cache the post-write gate against the pre-write hash and swallow
		// the change until something else happened to write. The cost of
		// the conservative choice is one redundant read; the cost of the
		// other is a board that silently stops updating.
		lastGate, lastHash = gate, hash
		mu.Unlock()
		return hash, nil
	}
}

// dbFilesSig is the cheap gate: size and mtime of the database and its WAL
// sidecars, with a marker for each that is absent so the string stays
// total across a store appearing or a WAL being checkpointed away.
func dbFilesSig(path string) string {
	var b strings.Builder
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if st, err := os.Stat(p); err == nil {
			fmt.Fprintf(&b, "%d:%d;", st.Size(), st.ModTime().UnixNano())
		} else {
			b.WriteString("-;")
		}
	}
	return b.String()
}

// storeArchiveMove records a dashboard archive/unarchive in the store — the
// same PutTicket+render every other store-backed write goes through, just
// triggered from the HTTP handler instead of a CLI verb. The render is what
// actually moves the file: BoardArchived decides which path the board task
// renders to, and RenderTo clears the one it left.
//
// It reports whether the store owned the move. Two shapes it does not: an id
// with no ticket at all, and a ticket that exists but is not board-tracked —
// the second is the subtler one, because the ticket resolves and setting a
// field on it would look like success while no card ever existed to move.
// Neither has a card, so the handler answers 404 for both. There is no third
// path any more: hand-dropped producer files stopped being supported input
// at adb-retire-devboard-dir, and the rename that served them went with the
// store-direct read path.
func storeArchiveMove(wd model.Workdir, id string, archived bool) (bool, error) {
	ss, err := openStoreForWrite(wd)
	if err != nil {
		return false, err
	}
	defer ss.close()

	t, err := ss.s.TicketBySlug(id)
	if store.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !t.BoardTracked {
		return false, nil
	}
	t.BoardArchived = archived
	if err := ss.commit(t); err != nil {
		return false, err
	}
	return true, nil
}
