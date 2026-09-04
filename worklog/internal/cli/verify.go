package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/prestontallen/ai-devboard/worklog/internal/devboard"
	"github.com/prestontallen/ai-devboard/worklog/internal/migrate"
	"github.com/prestontallen/ai-devboard/worklog/internal/store/memstore"
	"github.com/prestontallen/ai-devboard/worklog/internal/verify"
)

func newVerifyCmd() *cobra.Command {
	var flagJSON bool

	cmd := &cobra.Command{
		Use:           "verify",
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		Short:         "Report drift between live worklog/devboard data and the rewrite's rendered projections",
		Long: `verify is read-only: it converts your live worklog + devboard data into
an in-memory store, renders the worklog-rewrite's projections (WORK.md,
notes, archive, INDEX.md, devboard feed) from that store, and reports any
field-level differences against what's actually on disk. It never writes
to the live worklog or devboard directories.

This is a different concept from "worklog task phase verify" (a devboard
task's lifecycle phase) and from "worklog validate" (structural invariants
over live data, e.g. three-place epic/child consistency) — verify's only
concern is drift between live data and the rewrite's projections.

Exit codes:
  0  no drift
  1  error (I/O failure, torn read, refused conversion)
  2  drift found`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVerify(cmd, flagJSON)
		},
	}
	cmd.Flags().BoolVar(&flagJSON, "json", false, "emit a single JSON document (drift entries) instead of styled text")
	return cmd
}

func runVerify(cmd *cobra.Command, asJSON bool) error {
	wd, err := resolveWorkdir()
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}

	// Two stores: the corpus is converted into the first, and the
	// render is converted back into the second so verify can compare them
	// whole-struct (the oracle). verify stays interface-only, so the
	// composition root supplies both.
	s, s2 := memstore.New(), memstore.New()
	rep, err := verify.Run(s, s2, migrate.Sources{
		WorklogDir:  wd.Root,
		DevboardDir: devboard.DataDir(),
	})
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 1, "verify: %v", err)
	}

	if asJSON {
		if err := emitJSON(cmd.OutOrStdout(), verifyJSON{Clean: rep.Clean(), Drifts: orDrifts(rep.Drifts)}); err != nil {
			return errWithExit(1, "%v", err)
		}
		if !rep.Clean() {
			return errWithExit(2, "")
		}
		return nil
	}

	fmt.Fprint(cmd.OutOrStdout(), verifySummary(rep))
	if !rep.Clean() {
		return errWithExit(2, "")
	}
	return nil
}

// verifyJSON is the single JSON document criterion 10 requires.
type verifyJSON struct {
	Clean  bool           `json:"clean"`
	Drifts []verify.Drift `json:"drifts"`
}

func orDrifts(d []verify.Drift) []verify.Drift {
	if d == nil {
		return []verify.Drift{}
	}
	return d
}

// verifySummary renders the human-readable verdict (criterion 11): legible
// without --json.
func verifySummary(rep *verify.Report) string {
	var sb strings.Builder
	if rep.Clean() {
		fmt.Fprintln(&sb, "verify: clean — rendered projections match live data")
		return sb.String()
	}
	word := "entries"
	if len(rep.Drifts) == 1 {
		word = "entry"
	}
	fmt.Fprintf(&sb, "verify: %d drift %s found\n", len(rep.Drifts), word)
	for _, d := range rep.Drifts {
		if d.Ticket != "" {
			fmt.Fprintf(&sb, "  [%s] %s %s.%s: live=%q rendered=%q\n", d.Surface, d.File, d.Ticket, d.Field, d.Live, d.Rendered)
		} else {
			fmt.Fprintf(&sb, "  [%s] %s %s: live=%q rendered=%q\n", d.Surface, d.File, d.Field, d.Live, d.Rendered)
		}
	}
	return sb.String()
}
