package cli

import (
	"github.com/spf13/cobra"
)

func (a *App) searchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search MathWorld articles",
		Long: `Search Wolfram MathWorld for mathematical articles matching the query.

Results are returned in order of relevance and include the article title,
category, summary, and URL.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n := a.effectiveLimit(10)
			a.progressf("searching MathWorld for %q...", args[0])
			articles, err := a.client.Search(cmd.Context(), args[0], n)
			if err != nil {
				return codeError(exitError, err)
			}
			return a.renderOrEmpty(articles, len(articles))
		},
	}
	return cmd
}
