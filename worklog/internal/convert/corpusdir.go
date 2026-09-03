package convert

import (
	"os"
	"path/filepath"
	"strings"
)

// ReadCorpusDir loads a corpus from a directory tree:
//
//	WORK.md
//	FEEDBACK.md
//	archive/<YYYY-MM>.md
//	notes/<slug>.md
//	devboard/<repo>/<slug>.yaml (+ <repo>/_archive/<slug>.yaml)
//
// This is the exact layout of the live data dir (with devboard/ standing
// in for the separate devboard data root) and of rendered projections —
// the same reader serves the synthetic fixtures, the rendered output in
// the round-trip test, and the pinned live snapshot.
func ReadCorpusDir(root string) (Corpus, error) {
	c := Corpus{
		Archives: map[string][]byte{},
		Notes:    map[string][]byte{},
	}
	var err error
	if c.WorkMD, err = os.ReadFile(filepath.Join(root, "WORK.md")); err != nil {
		return c, err
	}
	if fb, err := os.ReadFile(filepath.Join(root, "FEEDBACK.md")); err == nil {
		c.Feedback = fb
	}
	if months, err := os.ReadDir(filepath.Join(root, "archive")); err == nil {
		for _, m := range months {
			if !strings.HasSuffix(m.Name(), ".md") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(root, "archive", m.Name()))
			if err != nil {
				return c, err
			}
			c.Archives[strings.TrimSuffix(m.Name(), ".md")] = data
		}
	}
	if notes, err := os.ReadDir(filepath.Join(root, "notes")); err == nil {
		for _, n := range notes {
			if !strings.HasSuffix(n.Name(), ".md") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(root, "notes", n.Name()))
			if err != nil {
				return c, err
			}
			c.Notes[strings.TrimSuffix(n.Name(), ".md")] = data
		}
	}
	boardRoot := filepath.Join(root, "devboard")
	if repos, err := os.ReadDir(boardRoot); err == nil {
		for _, repo := range repos {
			if !repo.IsDir() || strings.HasPrefix(repo.Name(), ".") {
				continue
			}
			addBoard := func(dir string, archived bool) error {
				files, err := os.ReadDir(dir)
				if err != nil {
					return nil // _archive may be absent
				}
				for _, f := range files {
					lower := strings.ToLower(f.Name())
					if !f.Type().IsRegular() ||
						(!strings.HasSuffix(lower, ".yaml") && !strings.HasSuffix(lower, ".yml")) {
						continue
					}
					data, err := os.ReadFile(filepath.Join(dir, f.Name()))
					if err != nil {
						return err
					}
					c.Board = append(c.Board, BoardInput{
						Repo: repo.Name(), Name: f.Name(), Archived: archived, Data: data,
					})
				}
				return nil
			}
			if err := addBoard(filepath.Join(boardRoot, repo.Name()), false); err != nil {
				return c, err
			}
			if err := addBoard(filepath.Join(boardRoot, repo.Name(), "_archive"), true); err != nil {
				return c, err
			}
		}
	}
	return c, nil
}
