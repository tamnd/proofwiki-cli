package cli

import (
	"github.com/spf13/cobra"
)

func (a *App) randomCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "random",
		Short: "Show random theorem pages from ProofWiki",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			n := a.effectiveLimit(5)
			a.progressf("fetching %d random pages...", n)
			pages, err := a.client.Random(cmd.Context(), n)
			if err != nil {
				return mapFetchErr(err)
			}
			return a.renderOrEmpty(pages, len(pages))
		},
	}
	return cmd
}
