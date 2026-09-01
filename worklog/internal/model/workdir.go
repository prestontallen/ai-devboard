package model

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// WorkDoc is a parsed WORK.md. After parsing it should be treated as
// read-only; the renderer mutates the on-disk file via line splices and
// re-parses if it needs an updated model.
type WorkDoc struct {
	Path     string
	Lines    []string // raw lines, no trailing newline
	Sections []Section

	// byID maps lowercase ID → pointer into Sections[i].Blocks[j].
	// Built by the parser; safe as long as the doc is not mutated post-parse.
	byID map[string]*Block
}

// Section returns the section with the given name, or nil.
func (d *WorkDoc) Section(name SectionName) *Section {
	for i := range d.Sections {
		if d.Sections[i].Name == name {
			return &d.Sections[i]
		}
	}
	return nil
}

// BlockByID returns the block with the given ID (case-insensitive), or nil.
func (d *WorkDoc) BlockByID(id string) *Block {
	if d.byID == nil {
		return nil
	}
	return d.byID[strings.ToLower(id)]
}

// SetByID is used by the parser to install the lookup table. Callers outside
// the parser should not invoke it.
func (d *WorkDoc) SetByID(m map[string]*Block) { d.byID = m }

// Workdir wraps the worklog data directory.
type Workdir struct {
	Root string
}

// NewWorkdir resolves root to an absolute path. Empty root → default of
// $XDG_DATA_HOME/worklog (or $HOME/.local/share/worklog if XDG isn't set).
// "~" prefix is expanded against the user's home directory.
func NewWorkdir(root string) (Workdir, error) {
	if root == "" {
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			root = filepath.Join(xdg, "worklog")
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return Workdir{}, err
			}
			root = filepath.Join(home, ".local", "share", "worklog")
		}
	} else if strings.HasPrefix(root, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return Workdir{}, err
		}
		root = filepath.Join(home, strings.TrimPrefix(root, "~"))
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return Workdir{}, err
	}
	return Workdir{Root: abs}, nil
}

func (w Workdir) WorkMD() string     { return filepath.Join(w.Root, "WORK.md") }
func (w Workdir) IndexMD() string    { return filepath.Join(w.Root, "INDEX.md") }
func (w Workdir) FeedbackMD() string { return filepath.Join(w.Root, "FEEDBACK.md") }
func (w Workdir) NotesDir() string   { return filepath.Join(w.Root, "notes") }
func (w Workdir) ArchiveDir() string { return filepath.Join(w.Root, "archive") }

// NotesFile returns the canonical path for an epic's notes file.
func (w Workdir) NotesFile(id string) string {
	return filepath.Join(w.NotesDir(), strings.ToLower(id)+".md")
}

// ArchiveFile returns the canonical path for a given YYYY-MM month.
func (w Workdir) ArchiveFile(yyyymm string) string {
	return filepath.Join(w.ArchiveDir(), yyyymm+".md")
}

// ErrWorkMDMissing is returned when the worklog data dir has no WORK.md.
var ErrWorkMDMissing = errors.New("WORK.md not found")
