package cli

import (
	"strings"

	"github.com/prestontallen/ai-devboard/worklog/internal/link"
	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/store"
)

// runStoreLink is link.SetLink's store-backed twin (adb-cutover M3d). The
// read/list paths (link.Get/link.List) stay on the legacy parser
// unconditionally, same rationale as pr's read path: write-through keeps
// WORK.md current, so parsing it is correct under either backend.
func runStoreLink(wd model.Workdir, id, name, value string) (link.Result, error) {
	ss, err := openStoreForWrite(wd)
	if err != nil {
		return link.Result{}, err
	}
	defer ss.close()

	t, err := ss.ticketBySlugOrErr(id, link.ErrIDNotFound)
	if err != nil {
		return link.Result{}, err
	}

	previous := ""
	idx := -1
	for i, l := range t.Links {
		if l.Kind == store.LinkRef && strings.EqualFold(l.Label, name) {
			idx = i
			previous = l.URL
			break
		}
	}

	switch {
	case value == "" && idx >= 0:
		t.Links = append(t.Links[:idx], t.Links[idx+1:]...)
	case value == "" && idx < 0:
		// Nothing to clear; leave Links untouched.
	case idx >= 0:
		t.Links[idx].URL = value
	default:
		t.Links = append(t.Links, store.Link{Kind: store.LinkRef, Label: name, URL: value})
	}

	if err := ss.commit(t); err != nil {
		return link.Result{}, err
	}

	var parentSlug string
	if t.ParentID != "" {
		if p, err := ss.s.Ticket(t.ParentID); err == nil {
			parentSlug = p.Slug
		}
	}

	return link.Result{ID: t.Slug, Name: name, URL: value, Previous: previous, Parent: parentSlug}, nil
}
