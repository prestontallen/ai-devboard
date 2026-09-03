package verify

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"

	"github.com/prestontallen/ai-devboard/worklog/internal/serve"
)

type boardTask struct {
	ID    string         `json:"id"`
	Notes string         `json:"notes"`
	Task  map[string]any `json:"task"`
}

type boardPayload struct {
	Repos []struct {
		Repo  string      `json:"repo"`
		Tasks []boardTask `json:"tasks"`
	} `json:"repos"`
}

// compareBoard is Decision #6: the /api/tasks payload builder is
// unexported, so both sides are compared over real HTTP against two
// ephemeral servers, exactly like TestOracles's serve-payload subtest.
func compareBoard(stageDir, renderDir string) []Drift {
	orig, err := fetchBoardPayload(stageDir)
	if err != nil {
		return []Drift{{Surface: "board", File: "devboard", Field: "fetch", Live: err.Error()}}
	}
	rendered, err := fetchBoardPayload(renderDir)
	if err != nil {
		return []Drift{{Surface: "board", File: "devboard", Field: "fetch", Rendered: err.Error()}}
	}
	return diffBoardPayloads(orig, rendered)
}

func fetchBoardPayload(root string) (boardPayload, error) {
	srv := serve.New(serve.Config{
		DataDir:    filepath.Join(root, "devboard"),
		WorklogDir: root,
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/tasks")
	if err != nil {
		return boardPayload{}, err
	}
	defer resp.Body.Close()

	var payload boardPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return boardPayload{}, err
	}
	return payload, nil
}

func diffBoardPayloads(orig, rendered boardPayload) []Drift {
	origByRepo := map[string]map[string]boardTask{}
	for _, g := range orig.Repos {
		m := make(map[string]boardTask, len(g.Tasks))
		for _, t := range g.Tasks {
			m[t.ID] = t
		}
		origByRepo[g.Repo] = m
	}
	renderedByRepo := map[string]map[string]boardTask{}
	for _, g := range rendered.Repos {
		m := make(map[string]boardTask, len(g.Tasks))
		for _, t := range g.Tasks {
			m[t.ID] = t
		}
		renderedByRepo[g.Repo] = m
	}

	var drifts []Drift
	for repo, origTasks := range origByRepo {
		renderedTasks, ok := renderedByRepo[repo]
		if !ok {
			drifts = append(drifts, Drift{Surface: "board", File: "devboard/" + repo, Field: "repo-presence", Live: "present", Rendered: "missing"})
			continue
		}
		for id, ot := range origTasks {
			rt, ok := renderedTasks[id]
			if !ok {
				drifts = append(drifts, Drift{Surface: "board", File: "devboard/" + repo, Ticket: id, Field: "presence", Live: "present", Rendered: "missing"})
				continue
			}
			delete(renderedTasks, id)
			if ot.Notes != rt.Notes {
				drifts = append(drifts, Drift{Surface: "board", File: "devboard/" + repo, Ticket: id, Field: "notes", Live: ot.Notes, Rendered: rt.Notes})
			}
			oj, _ := json.Marshal(ot.Task)
			rj, _ := json.Marshal(rt.Task)
			if string(oj) != string(rj) {
				drifts = append(drifts, Drift{Surface: "board", File: "devboard/" + repo, Ticket: id, Field: "task", Live: string(oj), Rendered: string(rj)})
			}
		}
		for id := range renderedTasks {
			drifts = append(drifts, Drift{Surface: "board", File: "devboard/" + repo, Ticket: id, Field: "presence", Live: "missing", Rendered: "present"})
		}
	}
	for repo := range renderedByRepo {
		if _, ok := origByRepo[repo]; !ok {
			drifts = append(drifts, Drift{Surface: "board", File: "devboard/" + repo, Field: "repo-presence", Live: "missing", Rendered: "present"})
		}
	}
	return dedupeDrifts(drifts)
}

// dedupeDrifts is defensive: map iteration order is random, but callers
// only ever build one Drift per (repo, ticket, field) key here, so this is
// a no-op in practice — kept because silent duplicate reporting would be
// a worse failure mode than a redundant pass.
func dedupeDrifts(in []Drift) []Drift {
	seen := map[string]bool{}
	out := make([]Drift, 0, len(in))
	for _, d := range in {
		key := fmt.Sprintf("%s|%s|%s|%s", d.Surface, d.File, d.Ticket, d.Field)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, d)
	}
	return out
}
