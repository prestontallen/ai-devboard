package serve

// Shadow mode (adb-store-serve-shadow): with cfg.StoreShadow set and a
// LoadStoreSnapshot injected, every /api/tasks request also builds the
// payload from the store's own rendering and logs where the two disagree.
// The response is always the file-built payload — this file is a
// measurement instrument, not a serving path. The flip to store-served is
// a later milestone, taken only once this diff has been driven to empty.
//
// The store side is a hybrid by design: the store renders board files only
// for tickets it owns, so a bare producer file (board YAML with no
// worklog: key) or an unparseable file — an error card — passes through
// file-derived on BOTH sides and can never be reported as drift. The
// equality claim is scoped to the store-owned subset; stating that here is
// what makes the milestone satisfiable rather than false by construction.

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/prestontallen/ai-devboard/worklog/internal/feedback"
	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/parse"
	"github.com/prestontallen/ai-devboard/worklog/internal/yamlx"
)

// shadowLogCap bounds per-report detail lines so a wholly divergent store
// cannot render the live log unreadable; the summary line always reports
// the full count, so nothing is dropped silently.
const shadowLogCap = 50

// runShadow compares filePayload against the store-built payload and logs
// the differences. It never modifies filePayload, never writes anything,
// and never lets a shadow failure reach the response — the board serves
// file-built bytes no matter what happens in here.
func (s *Server) runShadow(filePayload map[string]any) {
	defer func() {
		if r := recover(); r != nil {
			s.shadowFail(fmt.Sprintf("panic: %v", r))
		}
	}()
	snap, err := s.LoadStoreSnapshot()
	if err != nil {
		s.shadowFail(err.Error())
		return
	}
	if snap == nil {
		return // no store on this machine: silent no-op
	}
	s.shadowOK()
	s.reportShadow(diffPayloads(filePayload, s.allTasksStore(snap)))
}

// shadowFail logs a shadow breakdown once per distinct failure, not once
// per request — the board refetches on every change event, and a
// persistently locked store must not turn the log into a scroll.
func (s *Server) shadowFail(msg string) {
	s.mu.Lock()
	repeat := s.shadowLastErr == msg
	s.shadowLastErr = msg
	s.mu.Unlock()
	if !repeat {
		log.Printf("store-shadow: skipped, file payload served: %s", msg)
	}
}

func (s *Server) shadowOK() {
	s.mu.Lock()
	s.shadowLastErr = ""
	s.mu.Unlock()
}

// shadowDiff is one point of disagreement between the file-built and
// store-built payloads.
type shadowDiff struct {
	Repo     string // repo group the difference sits in ("" above repos[])
	Task     string // task entry id ("" outside a task entry)
	Path     string // JSON path, e.g. repos[1].tasks[0].task.phase
	Expected bool   // a known, converging divergence — not drift
	Detail   string // file=<v> store=<v>
}

// reportShadow logs one summary line plus one line per difference, capped
// at shadowLogCap. Identical consecutive reports are logged once: several
// board clients polling an unchanged disagreement should not repeat it.
func (s *Server) reportShadow(diffs []shadowDiff) {
	if len(diffs) == 0 {
		s.mu.Lock()
		s.shadowLastReport = ""
		s.mu.Unlock()
		return
	}
	lines := make([]string, 0, len(diffs)+1)
	expected := 0
	for _, d := range diffs {
		kind := "UNEXPECTED"
		if d.Expected {
			kind = "expected"
			expected++
		}
		lines = append(lines, fmt.Sprintf("store-shadow: %s repo=%q task=%q path=%s %s",
			kind, d.Repo, d.Task, d.Path, d.Detail))
	}
	report := strings.Join(lines, "\n")
	s.mu.Lock()
	repeat := s.shadowLastReport == report
	s.shadowLastReport = report
	s.mu.Unlock()
	if repeat {
		return
	}
	log.Printf("store-shadow: %d difference(s), %d expected, %d unexpected",
		len(diffs), expected, len(diffs)-expected)
	for i, line := range lines {
		if i == shadowLogCap {
			log.Printf("store-shadow: ...and %d more difference(s) not shown", len(lines)-shadowLogCap)
			break
		}
		log.Print(line)
	}
}

// allTasksStore builds the /api/tasks payload from the store's rendering —
// the same shape allTasks builds from disk. Store-owned board files come
// from the snapshot (content and mtime alike); anything on disk the store
// does not render — bare producer files, unparseable files — passes
// through parseTask, identically to the file side. A snapshot file with no
// disk counterpart is included too: the file side will lack it, and that
// missing entry is exactly the drift the shadow exists to catch.
func (s *Server) allTasksStore(snap *StoreSnapshot) map[string]any {
	names := map[string]bool{}
	if entries, err := os.ReadDir(s.cfg.DataDir); err == nil {
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				names[e.Name()] = true
			}
		}
	}
	for rel := range snap.Files {
		if rest, ok := strings.CutPrefix(rel, "devboard/"); ok {
			if repo, _, ok := strings.Cut(rest, "/"); ok {
				names[repo] = true
			}
		}
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)

	repos := make([]any, 0)
	for _, name := range sorted {
		tasks := make([]any, 0)
		for _, f := range s.unionTaskFiles(snap, name, false) {
			tasks = append(tasks, s.storeTaskEntry(snap, name, f, false))
		}
		for _, f := range s.unionTaskFiles(snap, name, true) {
			tasks = append(tasks, s.storeTaskEntry(snap, name, f, true))
		}
		if len(tasks) > 0 {
			repos = append(repos, map[string]any{"repo": name, "tasks": tasks})
		}
	}
	return map[string]any{
		"version":   s.currentVersion(),
		"generated": float64(time.Now().UnixNano()) / 1e9,
		"repos":     repos,
		"feedback":  s.storeFeedback(snap),
		"backlog":   s.storeBacklog(snap),
	}
}

// snapKey is the snapshot map key for a board file, mirroring the rel
// paths projection.Render emits.
func snapKey(repo, fname string, archived bool) string {
	if archived {
		return "devboard/" + repo + "/" + archiveDir + "/" + fname
	}
	return "devboard/" + repo + "/" + fname
}

// unionTaskFiles lists one repo dir's task files the way sortedTaskFiles
// does, merged with the snapshot's rendered files for that dir.
func (s *Server) unionTaskFiles(snap *StoreSnapshot, repo string, archived bool) []string {
	dir := filepath.Join(s.cfg.DataDir, repo)
	if archived {
		dir = filepath.Join(dir, archiveDir)
	}
	seen := map[string]bool{}
	for _, f := range sortedTaskFiles(dir) {
		seen[f] = true
	}
	prefix := strings.TrimPrefix(snapKey(repo, "", archived), "devboard/")
	for rel := range snap.Files {
		rest, ok := strings.CutPrefix(rel, "devboard/")
		if !ok || !strings.HasPrefix(rest, prefix) {
			continue
		}
		fname := strings.TrimPrefix(rest, prefix)
		if fname == "" || strings.Contains(fname, "/") {
			continue // a deeper path: _archive files seen from the live dir
		}
		seen[fname] = true
	}
	out := make([]string, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// storeTaskEntry is parseTask's store-side twin: same entry shape, content
// from the snapshot instead of disk, mtime from the ticket's
// BoardRenderedAt instead of a stat. Files the snapshot does not hold pass
// through parseTask untouched.
func (s *Server) storeTaskEntry(snap *StoreSnapshot, repo, fname string, archived bool) map[string]any {
	key := snapKey(repo, fname, archived)
	content, ok := snap.Files[key]
	if !ok {
		dir := filepath.Join(s.cfg.DataDir, repo)
		if archived {
			dir = filepath.Join(dir, archiveDir)
		}
		return s.parseTask(filepath.Join(dir, fname), archived)
	}

	rel := repo + "/" + fname
	if archived {
		rel = repo + "/" + archiveDir + "/" + fname
	}
	entry := map[string]any{
		"file": rel,
		"id":   strings.TrimSuffix(fname, filepath.Ext(fname)),
	}
	if archived {
		entry["archived"] = true
	}
	data, err := yamlx.YAMLToAny(content)
	if err != nil {
		entry["error"] = err.Error() // unreachable for renderer output; mirrors parseTask
		return entry
	}
	task, ok := data.(map[string]any)
	if !ok {
		entry["error"] = "top level must be a mapping"
		return entry
	}
	entry["task"] = task
	entry["mtime"] = float64(snap.BoardMTimes[key]) / 1e9
	if notes, ok := storeNotesFor(snap, task["worklog"]); ok {
		entry["notes"] = notes
	}
	storeAttachChildNotes(snap, task)
	return entry
}

// storeNotesFor is notesFor against the snapshot, same guard included so
// the two sides treat a malformed name identically.
func storeNotesFor(snap *StoreSnapshot, v any) (string, bool) {
	name, ok := v.(string)
	if !ok || name == "" || strings.ContainsAny(name, `/\`) || strings.Contains(name, "..") {
		return "", false
	}
	b, ok := snap.Files["notes/"+name+".md"]
	if !ok {
		return "", false
	}
	return string(b), true
}

func storeAttachChildNotes(snap *StoreSnapshot, task map[string]any) {
	children, ok := task["children"].([]any)
	if !ok {
		return
	}
	for _, c := range children {
		child, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if notes, ok := storeNotesFor(snap, child["id"]); ok {
			child["notes"] = notes
		}
	}
}

// storeBacklog is parseBacklog against the snapshot's WORK.md.
func (s *Server) storeBacklog(snap *StoreSnapshot) []backlogSection {
	out := make([]backlogSection, 0, len(backlogSections))
	doc, err := parse.Bytes("WORK.md", snap.Files["WORK.md"])
	for _, name := range backlogSections {
		section := backlogSection{Name: string(name), Items: []*model.Block{}}
		if err == nil {
			if found := doc.Section(name); found != nil {
				for i := range found.Blocks {
					section.Items = append(section.Items, &found.Blocks[i])
				}
			}
		}
		out = append(out, section)
	}
	return out
}

// storeFeedback is parseFeedback against the snapshot's FEEDBACK.md.
func (s *Server) storeFeedback(snap *StoreSnapshot) []feedbackView {
	out := make([]feedbackView, 0)
	entries, err := feedback.ParseBytes(snap.Files["FEEDBACK.md"])
	if err != nil {
		return out
	}
	for _, e := range entries {
		out = append(out, feedbackView{
			Timestamp: e.Timestamp,
			Signal:    string(e.Signal),
			Trigger:   e.Trigger,
			Excerpt:   e.Excerpt,
			Context:   e.Context,
			Resolved:  e.Resolved,
		})
	}
	return out
}

// diffPayloads compares the two payloads as the wire would see them: both
// are pushed through a JSON round-trip (so struct tags, number widths and
// map ordering cannot manufacture differences), version and generated are
// normalized away — the only two fields expected to differ — and the rest
// is walked order-sensitively. Order matters on purpose: verify's
// map-by-id board differ is blind to file, mtime, archived, ordering,
// feedback and backlog, which is exactly the blindness this milestone
// exists to remove.
func diffPayloads(filePayload, storePayload map[string]any) []shadowDiff {
	f := jsonRoundTrip(filePayload)
	st := jsonRoundTrip(storePayload)
	for _, k := range []string{"version", "generated"} {
		delete(f, k)
		delete(st, k)
	}
	var diffs []shadowDiff
	walkDiff("", "", "", f, st, &diffs)
	return diffs
}

func jsonRoundTrip(v map[string]any) map[string]any {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("store-shadow: payload not marshalable: %v", err))
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		panic(fmt.Sprintf("store-shadow: payload round-trip: %v", err))
	}
	return out
}

// walkDiff records every point where file and store disagree. repo and
// task label the enclosing repo group and task entry so a log line can be
// traced without re-deriving the path by hand.
func walkDiff(repo, task, path string, file, store any, out *[]shadowDiff) {
	emit := func(detail string, expected bool) {
		*out = append(*out, shadowDiff{Repo: repo, Task: task, Path: path, Expected: expected, Detail: detail})
	}
	switch fv := file.(type) {
	case map[string]any:
		sv, ok := store.(map[string]any)
		if !ok {
			emit(fmt.Sprintf("file=%s store=%s", short(file), short(store)), false)
			return
		}
		// Entering a task entry: pick up its id as the label.
		if id, ok := fv["id"].(string); ok && fv["task"] != nil {
			task = id
		}
		keys := map[string]bool{}
		for k := range fv {
			keys[k] = true
		}
		for k := range sv {
			keys[k] = true
		}
		sorted := make([]string, 0, len(keys))
		for k := range keys {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)
		for _, k := range sorted {
			p := path + "." + k
			if path == "" {
				p = k
			}
			fkv, fok := fv[k]
			skv, sok := sv[k]
			switch {
			case !fok:
				*out = append(*out, shadowDiff{Repo: repo, Task: task, Path: p, Detail: "missing on file side, store=" + short(skv)})
			case !sok:
				*out = append(*out, shadowDiff{Repo: repo, Task: task, Path: p, Detail: "missing on store side, file=" + short(fkv)})
			default:
				if k == "repo" && path != "" {
					if name, ok := fkv.(string); ok {
						repo = name
					}
				}
				walkDiff(repo, task, p, fkv, skv, out)
			}
		}
	case []any:
		sv, ok := store.([]any)
		if !ok {
			emit(fmt.Sprintf("file=%s store=%s", short(file), short(store)), false)
			return
		}
		// Pre-scan a repo group list so element diffs carry the label even
		// before the "repo" key is reached in sorted order.
		for i := 0; i < len(fv) || i < len(sv); i++ {
			p := fmt.Sprintf("%s[%d]", path, i)
			switch {
			case i >= len(fv):
				*out = append(*out, shadowDiff{Repo: repo, Task: task, Path: p, Detail: "missing on file side, store=" + short(sv[i])})
			case i >= len(sv):
				*out = append(*out, shadowDiff{Repo: repo, Task: task, Path: p, Detail: "missing on store side, file=" + short(fv[i])})
			default:
				elemRepo := repo
				if m, ok := fv[i].(map[string]any); ok {
					if name, ok := m["repo"].(string); ok {
						elemRepo = name
					}
				}
				walkDiff(elemRepo, task, p, fv[i], sv[i], out)
			}
		}
	default:
		if file != store {
			// The one converging divergence: a ticket from before the stamp
			// column existed serves mtime 0 store-side until its next
			// write-through render backfills it (see projection.RenderTo).
			expected := strings.HasSuffix(path, ".mtime") && store == float64(0)
			emit(fmt.Sprintf("file=%s store=%s", short(file), short(store)), expected)
		}
	}
}

// short renders a value for a log line, elided so one huge notes blob
// cannot swallow the log.
func short(v any) string {
	s := fmt.Sprintf("%v", v)
	if len(s) > 120 {
		s = s[:117] + "..."
	}
	return strconv.Quote(s)
}
