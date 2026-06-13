package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func (a *App) theoremCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "theorem <title>",
		Short: "Show a theorem page from ProofWiki",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			a.progressf("fetching %q...", args[0])
			th, err := a.client.Theorem(cmd.Context(), args[0])
			if err != nil {
				return mapFetchErr(err)
			}
			// raw format: print full content
			if a.output == string(FormatRaw) || a.output == "auto" {
				_, _ = fmt.Fprintf(os.Stdout, "# %s\n%s\n\n%s\n", th.Title, th.URL, th.Content)
				return nil
			}
			return a.render(th)
		},
	}
	return cmd
}
