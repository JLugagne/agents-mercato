package mercato

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/JLugagne/agents-mercato/internal/mercato/app"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/service"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/transform"
	"github.com/JLugagne/agents-mercato/internal/mercato/inbound/commands"
	"github.com/JLugagne/agents-mercato/internal/mercato/inbound/queries/tui"
	"github.com/JLugagne/agents-mercato/internal/mercato/outbound/cfgadapter"
	"github.com/JLugagne/agents-mercato/internal/mercato/outbound/fsadapter"
	"github.com/JLugagne/agents-mercato/internal/mercato/outbound/gitadapter"
	"github.com/JLugagne/agents-mercato/internal/mercato/outbound/txadapter"
	"github.com/JLugagne/agents-mercato/internal/mercato/update"
)

func resolveSSHEnabled(configPath string) bool {
	// Environment variable takes precedence
	if env := os.Getenv("MCT_SSH_ENABLED"); env != "" {
		return strings.EqualFold(env, "true") || env == "1"
	}
	// Check config file
	cfgStore := cfgadapter.NewConfigStore()
	cfg, err := cfgStore.Load(configPath)
	if err != nil {
		return false // default: disabled
	}
	if cfg.SSHEnabled != nil {
		return *cfg.SSHEnabled
	}
	return false // default: disabled
}

func NewApp(configPath, cacheDir string) *cobra.Command {
	sshEnabled := resolveSSHEnabled(configPath)
	gitRepo := gitadapter.New(gitadapter.WithSSHEnabled(sshEnabled))
	fs := fsadapter.New()
	cfgStore := cfgadapter.NewConfigStore()
	stateStore := cfgadapter.NewStateStore()

	installDB := cfgadapter.NewInstallDB()

	registry := transform.Registry()
	toolMappingsStore := cfgadapter.NewToolMappingStore()

	txm := txadapter.New(filepath.Join(cacheDir, "staging"))

	application := app.New(gitRepo, fs, cfgStore, stateStore, installDB, configPath, cacheDir,
		app.WithTransformers(registry),
		app.WithToolMappings(toolMappingsStore),
		app.WithTxManager(txm),
	)
	if err := application.RecoverPendingTx(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "mct: tx recovery:", err)
	}

	svc := commands.Services{
		Markets: application,
		Sync:    application,
		Entries: application,
		Search:  application,
		Readmes: application,
		Config:  application,
		Doctor:  application,
	}
	rootCmd := commands.NewRootCmd(svc)

	// updateResult receives the async update check result.
	var updateResult chan update.Result

	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if cmd.Name() == "init" || cmd.Name() == "help" || cmd.Name() == "version" || cmd.Name() == "dist-upgrade" {
			return nil
		}
		if !cfgStore.Exists(configPath) {
			_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "First run detected — initializing mct…")
			return application.Init(service.InitOpts{LocalPath: ".claude/"})
		}

		// Async update check (at most once per day).
		if update.ShouldCheck(cacheDir) {
			updateResult = make(chan update.Result, 1)
			go func() {
				updateResult <- update.CheckLatestVersion(cacheDir, commands.Version)
			}()
		}
		return nil
	}

	rootCmd.PersistentPostRunE = func(cmd *cobra.Command, args []string) error {
		if updateResult == nil {
			return nil
		}
		r := <-updateResult
		if !r.UpdateAvailable {
			return nil
		}
		// Skip notice for JSON output and quiet mode.
		jsonOut, _ := cmd.Flags().GetBool("json")
		quiet, _ := cmd.Flags().GetBool("quiet")
		if jsonOut || quiet {
			return nil
		}
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), update.FormatUpdateNotice(r))
		return nil
	}

	rootCmd.AddCommand(newTUICmd(application, cacheDir))

	return rootCmd
}

func newTUICmd(application *app.App, cacheDir string) *cobra.Command {
	return &cobra.Command{
		Use:   "tui",
		Short: "Launch interactive TUI",
		RunE: func(cmd *cobra.Command, args []string) error {
			r := update.CachedResult(cacheDir, commands.Version)
			return tui.RunTUI(tui.TUIServices{
				Markets: application,
				Sync:    application,
				Entries: application,
				Search:  application,
			}, r.UpdateAvailable, r.LatestVersion)
		},
	}
}
