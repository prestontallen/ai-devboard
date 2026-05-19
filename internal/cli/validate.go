package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/prestontallen/day2day/internal/style"
	"github.com/prestontallen/day2day/internal/validate"
)

type jsonValidateViolation struct {
	Check   string `json:"check"`
	Message string `json:"message"`
}

type jsonValidate struct {
	Dir            string                  `json:"dir"`
	WorkMDMissing  bool                    `json:"workMDMissing"`
	Violations     []jsonValidateViolation `json:"violations"`
	Infos          []string                `json:"infos"`
	ViolationCount int                     `json:"violationCount"`
}

func newValidateCmd() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "validate",
		Args:  cobra.NoArgs,
		Short: "Run all structural invariants over the worklog data dir",
		Long: `validate runs the same checks the bash validate.sh did, plus a full
implementation of the three-place-consistency rule. Exit codes:
  0  no violations
  1  WORK.md missing
  2  one or more violations

With --json, the result is emitted as a single JSON object on stdout
(including any error path) so callers can parse a single document
regardless of outcome.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runValidate(cmd, asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit a JSON result object instead of styled text")
	return cmd
}

func runValidate(cmd *cobra.Command, asJSON bool) error {
	wd, err := resolveWorkdir()
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}

	res, err := validate.Run(wd)
	if err != nil {
		return jsonOrTextError(cmd, asJSON, 1, "%v", err)
	}

	if asJSON {
		payload := jsonValidate{
			Dir:            wd.Root,
			WorkMDMissing:  res.WorkMDMissing,
			Violations:     toJSONViolations(res.Violations),
			Infos:          res.Infos,
			ViolationCount: len(res.Violations),
		}
		if payload.Violations == nil {
			payload.Violations = []jsonValidateViolation{}
		}
		if payload.Infos == nil {
			payload.Infos = []string{}
		}
		if err := emitJSON(cmd.OutOrStdout(), payload); err != nil {
			return errWithExit(1, "%v", err)
		}
		switch {
		case res.WorkMDMissing:
			return errWithExit(1, "")
		case len(res.Violations) > 0:
			return errWithExit(2, "")
		}
		return nil
	}

	// Styled text mode (unchanged behavior).
	for _, info := range res.Infos {
		fmt.Fprintln(cmd.ErrOrStderr(), style.Dim.Render("INFO: "+info))
	}
	for _, v := range res.Violations {
		fmt.Fprintln(cmd.ErrOrStderr(),
			style.Bad.Render("VIOLATION ")+
				style.SubHeading.Render("["+string(v.Check)+"]")+" "+v.Message)
	}

	count := len(res.Violations)
	if count == 0 {
		fmt.Fprintln(cmd.OutOrStdout(),
			style.Good.Render(fmt.Sprintf("validate: 0 violations in %s", wd.Root)))
		return nil
	}
	fmt.Fprintln(cmd.OutOrStdout(),
		style.Bad.Render(fmt.Sprintf("validate: %d violation(s) in %s", count, wd.Root)))

	if res.WorkMDMissing {
		return errWithExit(1, "")
	}
	return errWithExit(2, "")
}

func toJSONViolations(in []validate.Violation) []jsonValidateViolation {
	out := make([]jsonValidateViolation, 0, len(in))
	for _, v := range in {
		out = append(out, jsonValidateViolation{
			Check:   string(v.Check),
			Message: v.Message,
		})
	}
	return out
}
