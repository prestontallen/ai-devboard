package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newPingCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ping",
		Args:  cobra.NoArgs,
		Short: "Print pong (dev-context workflow drill)",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), "pong")
			return nil
		},
	}
}
