package secrets

import (
	"encoding/json"
	"fmt"

	"alsayf/internal/secrets"
	"github.com/spf13/cobra"
)

func newRulesCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "rules", Short: "Inspect embedded detection rules"}
	var format string
	list := &cobra.Command{
		Use: "list", Short: "List enabled secret detection rules", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if format != "text" && format != "json" {
				return fmt.Errorf("unsupported format %q (use text or json)", format)
			}
			rules, err := secrets.Rules()
			if err != nil {
				return err
			}
			if format == "json" {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(rules)
			}
			for _, r := range rules {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", r.ID, r.Name); err != nil {
					return err
				}
			}
			return nil
		},
	}
	list.Flags().StringVar(&format, "format", "text", "Output format: text or json")
	cmd.AddCommand(list)
	return cmd
}
