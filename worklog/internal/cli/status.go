package cli

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/prestontallen/ai-devboard/worklog/internal/model"
	"github.com/prestontallen/ai-devboard/worklog/internal/parse"
	"github.com/prestontallen/ai-devboard/worklog/internal/style"
	"github.com/prestontallen/ai-devboard/worklog/internal/validate"
)

// JSON wire types — kept local to the CLI because they translate model
// internals (raw State, line numbers) into agent-friendly shapes.

type jsonBlock struct {
	ID             string   `json:"id"`
	State          string   `json:"state"`
	Title          string   `json:"title"`
	Type           string   `json:"type"`
	Parent         string   `json:"parent"`
	Repo           string   `json:"repo"`
	Tags           []string `json:"tags"`
	Started        string   `json:"started"`
	PR             string   `json:"pr"`
	WaitingSince   string   `json:"waitingSince,omitempty"`
	WaitingDays    int      `json:"waitingDays,omitempty"`
	Files          []string `json:"files"`
	Acceptance     string   `json:"acceptance"`
	NotesRef       string   `json:"notesRef"`
	Status         string   `json:"status"`
	ActiveChildren []string `json:"activeChildren"`
}

type jsonSection struct {
	Name   string      `json:"name"`
	Count  int         `json:"count"`
	Cap    int         `json:"cap"`
	Blocks []jsonBlock `json:"blocks"`
}

type jsonStatus struct {
	Dir      string        `json:"dir"`
	Sections []jsonSection `json:"sections"`
}

func newStatusCmd() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "status",
		Args:  cobra.NoArgs,
		Short: "Print Now/Next/Someday in a styled, scriptable summary",
		Long: `status prints a snapshot of the worklog's front page.

By default the output is Lip Gloss-styled text for humans. With --json
the same data is emitted as a structured JSON document (including the
error path) so agents can parse a single value regardless of outcome.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(cmd, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a JSON document instead of styled text")
	return cmd
}

func runStatus(cmd *cobra.Command, asJSON bool) error {
	wd, err := resolveWorkdir()
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}

	doc, err := parse.File(wd.WorkMD())
	if err != nil {
		if errors.Is(err, model.ErrWorkMDMissing) {
			return jsonOrTextError(cmd, asJSON, 1, "WORK.md not found at %s", wd.WorkMD())
		}
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}

	if asJSON {
		return emitStatusJSON(cmd, wd.Root, doc)
	}
	return emitStatusText(cmd, doc)
}

func emitStatusJSON(cmd *cobra.Command, dir string, doc *model.WorkDoc) error {
	payload := jsonStatus{
		Dir: dir,
		Sections: []jsonSection{
			buildJSONSection(doc, model.SectionNow, validate.NowCap),
			buildJSONSection(doc, model.SectionWaiting, 0),
			buildJSONSection(doc, model.SectionNext, 0),
			buildJSONSection(doc, model.SectionSomeday, 0),
		},
	}
	return emitJSON(cmd.OutOrStdout(), payload)
}

func buildJSONSection(doc *model.WorkDoc, name model.SectionName, cap int) jsonSection {
	out := jsonSection{
		Name:   string(name),
		Cap:    cap,
		Blocks: []jsonBlock{},
	}
	sec := doc.Section(name)
	if sec == nil {
		return out
	}
	out.Count = len(sec.Blocks)
	for _, b := range sec.Blocks {
		out.Blocks = append(out.Blocks, toJSONBlock(b))
	}
	return out
}

func toJSONBlock(b model.Block) jsonBlock {
	return jsonBlock{
		ID:             b.ID,
		State:          b.State.Label(),
		Title:          b.Title,
		Type:           string(b.Type),
		Parent:         b.Parent,
		Repo:           b.Repo,
		Tags:           defaultStringSlice(b.Tags),
		Started:        b.Started,
		PR:             b.PR,
		WaitingSince:   b.WaitingSince,
		WaitingDays:    waitingAge(b.WaitingSince, time.Now()),
		Files:          defaultStringSlice(b.Files),
		Acceptance:     b.Acceptance,
		NotesRef:       b.NotesRef,
		Status:         b.Status,
		ActiveChildren: defaultStringSlice(b.ActiveChildren),
	}
}

func waitingAge(since string, now time.Time) int {
	if since == "" {
		return 0
	}
	t, err := time.Parse("2006-01-02", since)
	if err != nil {
		return 0
	}
	days := int(now.Truncate(24*time.Hour).Sub(t.Truncate(24*time.Hour)).Hours() / 24)
	if days < 0 {
		return 0
	}
	return days
}

// defaultStringSlice replaces a nil slice with an empty one so JSON renders
// `[]` instead of `null`. Stable schemas help agents parse with confidence.
func defaultStringSlice(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func emitStatusText(cmd *cobra.Command, doc *model.WorkDoc) error {
	out := cmd.OutOrStdout()
	now := doc.Section(model.SectionNow)
	next := doc.Section(model.SectionNext)
	someday := doc.Section(model.SectionSomeday)

	// ## Now header with cap indicator
	nowCount := 0
	if now != nil {
		nowCount = len(now.Blocks)
	}
	capStyle := style.CapColor(nowCount, validate.NowCap)
	fmt.Fprintln(out, style.Heading.Render("## Now ")+
		capStyle.Render(fmt.Sprintf("(%d of %d)", nowCount, validate.NowCap)))
	if now != nil {
		for _, b := range now.Blocks {
			fmt.Fprintln(out, "  "+renderBlockLine(b))
		}
		if len(now.Blocks) == 0 {
			fmt.Fprintln(out, "  "+style.Dim.Render("<empty>"))
		}
	}
	fmt.Fprintln(out)

	// ## Waiting — with per-ticket age annotation
	waiting := doc.Section(model.SectionWaiting)
	if waiting != nil && len(waiting.Blocks) > 0 {
		fmt.Fprintln(out, style.Heading.Render("## Waiting"))
		today := time.Now()
		maxAge := 0
		for _, b := range waiting.Blocks {
			age := waitingAge(b.WaitingSince, today)
			if age > maxAge {
				maxAge = age
			}
			line := "  " + renderBlockLine(b)
			if age > 0 {
				line += " " + style.Dim.Render(fmt.Sprintf("(%d days)", age))
			}
			fmt.Fprintln(out, line)
		}
		if maxAge > 0 {
			fmt.Fprintln(out, "  "+style.Warn.Render(fmt.Sprintf("oldest waiting %d days", maxAge)))
		}
		fmt.Fprintln(out)
	}

	// ## Next — list epics with their Active children, then standalone tickets
	fmt.Fprintln(out, style.Heading.Render("## Next"))
	if next == nil || len(next.Blocks) == 0 {
		fmt.Fprintln(out, "  "+style.Dim.Render("<empty>"))
	} else {
		for _, b := range next.Blocks {
			fmt.Fprintln(out, "  "+renderBlockLine(b))
			if b.IsEpic() && len(b.ActiveChildren) > 0 {
				fmt.Fprintln(out, "    "+style.SubHeading.Render("active children: ")+
					style.Active.Render(strings.Join(b.ActiveChildren, ", ")))
			}
		}
	}
	fmt.Fprintln(out)

	// ## Someday — just a count
	somedayCount := 0
	if someday != nil {
		somedayCount = len(someday.Blocks)
	}
	fmt.Fprintln(out, style.Heading.Render("## Someday ")+
		style.Dim.Render(fmt.Sprintf("(%d items)", somedayCount)))
	return nil
}

func renderBlockLine(b model.Block) string {
	var stateBox string
	switch b.State {
	case model.StateActive:
		stateBox = style.Active.Render("[~]")
	case model.StateDone:
		stateBox = style.Done.Render("[x]")
	default:
		stateBox = style.Pending.Render("[ ]")
	}

	var idStyle string
	if b.IsEpic() {
		idStyle = style.Epic.Render(strings.ToUpper(b.ID))
	} else {
		idStyle = style.SubHeading.Render(strings.ToUpper(b.ID))
	}

	parts := []string{stateBox, idStyle}
	if b.Title != "" {
		parts = append(parts, b.Title)
	}
	if b.Parent != "" {
		parts = append(parts, style.Dim.Render("(parent: "+b.Parent+")"))
	}
	if len(b.Tags) > 0 {
		var tagStr []string
		for _, t := range b.Tags {
			tagStr = append(tagStr, style.Tag.Render(t))
		}
		parts = append(parts, strings.Join(tagStr, " "))
	}
	return strings.Join(parts, " ")
}
