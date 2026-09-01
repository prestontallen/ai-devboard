package model

// Section is a `## <Name>` block in WORK.md and its contents.
//
// Line ranges are JSON-skipped; CLI wire types expose count + the parsed
// blocks rather than raw offsets.
type Section struct {
	Name     SectionName `json:"name"`
	HeadLine int         `json:"-"`
	EndLine  int         `json:"-"`
	Blocks   []Block     `json:"blocks"`
}

// FindBlock returns a pointer to the block with the matching ID (case-
// insensitive), or nil if not present.
func (s *Section) FindBlock(id string) *Block {
	for i := range s.Blocks {
		if equalFoldID(s.Blocks[i].ID, id) {
			return &s.Blocks[i]
		}
	}
	return nil
}

func equalFoldID(a, b string) bool {
	// IDs are lowercase-kebab; the parser already lowercases them, but be
	// defensive in case a caller passes mixed case.
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
