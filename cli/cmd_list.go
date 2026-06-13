package cli

import (
	"github.com/spf13/cobra"
)

func (a *App) listCmd() *cobra.Command {
	var category string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List pages in a ProofWiki category",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			n := a.effectiveLimit(20)
			a.progressf("listing category %q...", category)
			pages, err := a.client.ListCategory(cmd.Context(), category, n)
			if err != nil {
				return mapFetchErr(err)
			}
			return a.renderOrEmpty(pages, len(pages))
		},
	}
	cmd.Flags().StringVar(&category, "category", "Theorems", "category name to list (without Category: prefix)")
	return cmd
}
