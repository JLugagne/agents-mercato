package commands

import (
	"github.com/spf13/cobra"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain/service"
)

func newMigrateCmd(svc Services, opts *GlobalOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Convert files written by older mct versions to the current layout",
		Long: `Convert files written by older mct versions to the current layout:
single-file skills stored without extension, legacy hook entries in
.claude/settings.json, and outputs of other tools from previous formats or
removed tools. Locally modified files are kept. Runs automatically at the
start of update and sync for the current project.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			allLocations, _ := cmd.Flags().GetBool("all-locations")
			jsonOut, _ := cmd.Flags().GetBool("json")
			results, err := svc.Sync.Migrate(service.MigrateOpts{AllLocations: allLocations, DryRun: dryRun})
			if err != nil {
				return err
			}
			if jsonOut {
				return printJSON(cmd.OutOrStdout(), results)
			}
			if len(results) == 0 {
				cmd.Println("  ok  nothing to migrate")
				return nil
			}
			for _, r := range results {
				label := r.Location
				if r.Market != "" {
					label = r.Market + "@" + r.Profile + "  " + r.Location
				}
				if r.Err != nil {
					cmd.PrintErrf("  x  %s: %v\n", label, r.Err)
					continue
				}
				cmd.Printf("  migrated  %s\n", label)
				for _, a := range r.Actions {
					cmd.Printf("     %s\n", a)
				}
				for _, w := range r.Warnings {
					cmd.PrintErrf("     !  %s\n", w)
				}
			}
			return nil
		},
	}
	cmd.Flags().Bool("dry-run", false, "show what would be migrated without writing")
	cmd.Flags().Bool("all-locations", false, "migrate every project that has installed entries, not only the current one")
	cmd.Flags().Bool("json", false, "JSON output")
	return cmd
}
