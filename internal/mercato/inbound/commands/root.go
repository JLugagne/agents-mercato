package commands

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain/service"
)

// Version is set at build time via -ldflags "-X ...commands.Version=vX.Y.Z".
// Defaults to "dev" for local builds.
var Version = "dev"

// GlobalOpts holds flags shared across all commands
type GlobalOpts struct {
	ConfigPath string
	CacheDir   string
	Offline    bool
	Verbose    bool
	Quiet      bool
	NoColor    bool
	CI         bool
}

// Services groups all service interfaces for command handlers
type Services struct {
	Markets service.MarketCommands
	Sync    service.SyncCommands
	Entries service.EntryCommands
	Search  service.SearchQueries
	Readmes service.ReadmeQueries
	Config  service.ConfigCommands
	Doctor  service.DoctorQueries
}

// NewRootCmd builds the root cobra command with all subcommands
func NewRootCmd(svc Services) *cobra.Command {
	opts := &GlobalOpts{}

	root := &cobra.Command{
		Use:           "mct",
		Short:         "Claude agent and skill market manager",
		Long:          "agents-mercato — manage Claude agent and skill definitions across Git-based markets",
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       Version,
	}

	// Global flags
	root.PersistentFlags().StringVar(&opts.ConfigPath, "config", defaultConfigPath(), "path to config file")
	root.PersistentFlags().StringVar(&opts.CacheDir, "cache", defaultCacheDir(), "cache directory")
	root.PersistentFlags().BoolVar(&opts.Offline, "offline", false, "disable network operations")
	root.PersistentFlags().BoolVar(&opts.Verbose, "verbose", false, "detailed output")
	root.PersistentFlags().BoolVar(&opts.Quiet, "quiet", false, "suppress all output except errors")
	root.PersistentFlags().BoolVar(&opts.NoColor, "no-color", false, "disable ANSI colours")
	root.PersistentFlags().BoolVar(&opts.CI, "ci", false, "non-interactive mode")

	// Register subcommands
	root.AddCommand(
		newMarketCmd(svc, opts),
		newRefreshCmd(svc, opts),
		newUpdateCmd(svc, opts),
		newSyncCmd(svc, opts),
		newCheckCmd(svc, opts),
		newAddCmd(svc, opts),
		newRemoveCmd(svc, opts),
		newPruneCmd(svc, opts),
		newSearchCmd(svc, opts),
		newListCmd(svc, opts),
		newConflictsCmd(svc, opts),
		newSyncStateCmd(svc, opts),
		newIndexCmd(svc, opts),
		newInitCmd(svc, opts),
		newReadmeCmd(svc, opts),
		newConfigCmd(svc, opts),
		newExportCmd(svc, opts),
		newImportCmd(svc, opts),
		newLintCmd(svc, opts),
		newHookCmd(svc, opts),
		newDoctorCmd(svc, opts),
		newUpgradeCmd(opts),
	)

	// Aliases
	root.AddCommand(newStatusCmd(svc, opts))  // alias for check
	root.AddCommand(newInstallCmd(svc, opts)) // alias for add
	root.AddCommand(newMarketsCmd(svc, opts)) // alias for market list
	root.AddCommand(newSaveCmd(svc, opts))    // alias for export .mct.json
	root.AddCommand(newRestoreCmd(svc, opts)) // alias for import .mct.json
	root.AddCommand(newDistUpgradeCmd(opts))  // alias for upgrade

	return root
}

func defaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "~/.config/mct/config.yml"
	}
	return filepath.Join(home, ".config", "mct", "config.yml")
}

func defaultCacheDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "~/.cache/mct"
	}
	return filepath.Join(home, ".cache", "mct")
}
