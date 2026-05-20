package commands

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
	"github.com/JLugagne/agents-mercato/internal/mercato/domain/service"
)

func newRefreshCmd(svc Services, opts *GlobalOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "refresh",
		Short: "Fetch latest from all markets",
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonOut, _ := cmd.Flags().GetBool("json")
			results, err := svc.Sync.Refresh(service.RefreshOpts{CI: opts.CI})
			if err != nil {
				return err
			}
			if jsonOut {
				return printJSON(cmd.OutOrStdout(), results)
			}
			for _, r := range results {
				for _, p := range r.PrunedLocations {
					cmd.Printf("  --  pruned stale location: %s\n", p)
				}
				for _, p := range r.PrunedFiles {
					cmd.Printf("  --  pruned upstream-removed: %s\n", p)
				}
				if r.Market == "" {
					continue
				}
				if r.Err != nil {
					cmd.PrintErrf("  x  %s: %v\n", r.Market, r.Err)
					continue
				}
				if r.OldSHA == r.NewSHA {
					cmd.Printf("  ok  %s (up to date at %s)\n", r.Market, r.NewSHA[:7])
				} else {
					upd := ""
					if r.UpdatesAvailable > 0 {
						upd = fmt.Sprintf(", %d updates available", r.UpdatesAvailable)
					}
					cmd.Printf("  up  %s %s -> %s (%d files changed%s)\n", r.Market, r.OldSHA[:7], r.NewSHA[:7], r.ChangedFiles, upd)
				}
			}
			return nil
		},
	}
	cmd.Flags().Bool("json", false, "JSON output")
	return cmd
}

func newUpdateCmd(svc Services, opts *GlobalOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Apply pending changes to local files",
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, _ := cmd.Flags().GetString("ref")
			market, _ := cmd.Flags().GetString("market")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			agentsOnly, _ := cmd.Flags().GetBool("agents-only")
			skillsOnly, _ := cmd.Flags().GetBool("skills-only")
			allKeep, _ := cmd.Flags().GetBool("all-keep")
			allDelete, _ := cmd.Flags().GetBool("all-delete")
			allMerge, _ := cmd.Flags().GetBool("all-merge")
			acceptBreaking, _ := cmd.Flags().GetBool("accept-breaking")
			jsonOut, _ := cmd.Flags().GetBool("json")
			results, err := svc.Sync.Update(service.UpdateOpts{
				Ref:            domain.MctRef(ref),
				Market:         market,
				DryRun:         dryRun,
				AgentsOnly:     agentsOnly,
				SkillsOnly:     skillsOnly,
				AllKeep:        allKeep,
				AllDelete:      allDelete,
				AllMerge:       allMerge,
				AcceptBreaking: acceptBreaking,
				CI:             opts.CI,
			})
			if err != nil {
				return err
			}
			if jsonOut {
				return printJSON(cmd.OutOrStdout(), results)
			}
			for _, r := range results {
				if r.Err != nil {
					cmd.PrintErrf("  x  %s: %v\n", r.Ref, r.Err)
				} else if len(r.DriftFiles) > 0 && r.Action == "drift" {
					cmd.Printf("  ~  %s has local changes in: %s\n", r.Ref, strings.Join(r.DriftFiles, ", "))
				} else if r.Action == "kept" {
					cmd.Printf("  ~  %s kept (local changes preserved)\n", r.Ref)
				} else {
					cmd.Printf("  %s  %s %s -> %s\n", r.Action, r.Ref, r.OldVersion, r.NewVersion)
				}
			}
			return nil
		},
	}
	cmd.Flags().String("ref", "", "specific entry ref")
	cmd.Flags().String("market", "", "filter to market")
	cmd.Flags().Bool("dry-run", false, "preview changes")
	cmd.Flags().Bool("agents-only", false, "only update agents")
	cmd.Flags().Bool("skills-only", false, "only update skills")
	cmd.Flags().Bool("all-keep", false, "keep all local changes")
	cmd.Flags().Bool("all-delete", false, "delete all local changes")
	cmd.Flags().Bool("all-merge", false, "merge all changes")
	cmd.Flags().Bool("accept-breaking", false, "accept breaking changes")
	cmd.Flags().Bool("json", false, "JSON output")
	return cmd
}

func newSyncCmd(svc Services, opts *GlobalOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Refresh, restore deleted files (interactive), then update",
		RunE: func(cmd *cobra.Command, args []string) error {
			market, _ := cmd.Flags().GetString("market")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			acceptBreaking, _ := cmd.Flags().GetBool("accept-breaking")
			allMerge, _ := cmd.Flags().GetBool("all-merge")
			allLocations, _ := cmd.Flags().GetBool("all")
			restoreAll, _ := cmd.Flags().GetBool("restore-all")
			restoreNone, _ := cmd.Flags().GetBool("restore-none")
			jsonOut, _ := cmd.Flags().GetBool("json")

			// Phase 1: refresh (fetch + prune stale locations + prune upstream-removed).
			refreshResults, err := svc.Sync.Refresh(service.RefreshOpts{
				Market: market,
				DryRun: dryRun,
				CI:     opts.CI,
			})
			if err != nil {
				return err
			}

			// Phase 2: detect locally-deleted files and offer to restore them.
			var restored []service.RestoreResult
			if !dryRun {
				deleted, err := svc.Sync.DetectDeleted(service.DetectDeletedOpts{Market: market})
				if err != nil {
					return err
				}
				if len(deleted) > 0 {
					toRestore := chooseFilesToRestore(cmd, deleted, restoreFlags{
						all:  restoreAll,
						none: restoreNone,
						ci:   opts.CI,
					})
					if len(toRestore) > 0 {
						restored, err = svc.Sync.RestoreDeleted(toRestore, service.RestoreOpts{})
						if err != nil {
							return err
						}
					}
				}
			}

			// Phase 3: update.
			updateResults, err := svc.Sync.Update(service.UpdateOpts{
				Market:         market,
				DryRun:         dryRun,
				CI:             opts.CI,
				AcceptBreaking: acceptBreaking,
				AllMerge:       allMerge,
				AllLocations:   allLocations,
			})
			if err != nil {
				return err
			}

			// Stitch into SyncResult shape for output parity with svc.Sync.Sync.
			updatesByMarket := make(map[string][]service.UpdateResult)
			for _, u := range updateResults {
				updatesByMarket[u.Ref.Market()] = append(updatesByMarket[u.Ref.Market()], u)
			}
			results := make([]service.SyncResult, 0, len(refreshResults))
			for i, r := range refreshResults {
				sr := service.SyncResult{Refresh: r, Updates: updatesByMarket[r.Market]}
				if i == 0 {
					sr.Restored = restored
				}
				results = append(results, sr)
			}

			if jsonOut {
				return printJSON(cmd.OutOrStdout(), results)
			}
			for _, r := range results {
				for _, p := range r.Refresh.PrunedLocations {
					cmd.Printf("  --  pruned stale location: %s\n", p)
				}
				for _, p := range r.Refresh.PrunedFiles {
					cmd.Printf("  --  pruned upstream-removed: %s\n", p)
				}
				for _, rr := range r.Restored {
					switch rr.Action {
					case "restored":
						cmd.Printf("  ++  restored %s in %s\n", rr.File.RelPath, rr.File.Location)
					case "would_restore":
						cmd.Printf("  ?   would restore %s in %s\n", rr.File.RelPath, rr.File.Location)
					case "failed":
						cmd.PrintErrf("  x  restore failed for %s: %v\n", rr.File.RelPath, rr.Err)
					}
				}
				if r.Refresh.Market == "" {
					continue
				}
				if r.Refresh.Err != nil {
					cmd.PrintErrf("  x  %s: %v\n", r.Refresh.Market, r.Refresh.Err)
				} else {
					cmd.Printf("  up  %s %s -> %s\n", r.Refresh.Market, r.Refresh.OldSHA[:7], r.Refresh.NewSHA[:7])
				}
				for _, u := range r.Updates {
					cmd.Printf("     %s  %s  %s\n", u.Action, u.Ref, u.Location)
				}
			}
			return nil
		},
	}
	cmd.Flags().String("market", "", "filter to market")
	cmd.Flags().Bool("dry-run", false, "preview changes")
	cmd.Flags().Bool("accept-breaking", false, "accept breaking changes")
	cmd.Flags().Bool("all-merge", false, "merge all changes")
	cmd.Flags().Bool("all", false, "update all locations where entries are installed")
	cmd.Flags().Bool("restore-all", false, "restore every locally-deleted file without prompting")
	cmd.Flags().Bool("restore-none", false, "skip restoring locally-deleted files (no prompt)")
	cmd.Flags().Bool("json", false, "JSON output")
	return cmd
}

func newCheckCmd(svc Services, opts *GlobalOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Show status of installed entries",
		RunE: func(cmd *cobra.Command, args []string) error {
			market, _ := cmd.Flags().GetString("market")
			jsonOut, _ := cmd.Flags().GetBool("json")
			statuses, err := svc.Sync.Check(service.CheckOpts{
				Market: market,
				JSON:   jsonOut,
				CI:     opts.CI,
			})
			if err != nil {
				return err
			}
			if jsonOut {
				return printJSON(cmd.OutOrStdout(), statuses)
			}
			indicators := map[domain.EntryState]string{
				domain.StateClean:           "ok",
				domain.StateUpdateAvailable: "upd",
				domain.StateDrift:           "~",
				domain.StateUpdateAndDrift:  "!",
				domain.StateDeleted:         "x",
				domain.StateNewInRegistry:   "+",
				domain.StateOrphaned:        "o",
				domain.StateUnknown:         "?",
			}
			for _, s := range statuses {
				ind := indicators[s.State]
				if len(s.ToolStates) > 0 {
					var toolParts []string
					for tool, state := range s.ToolStates {
						toolParts = append(toolParts, fmt.Sprintf("%s:%s", tool, state))
					}
					cmd.Printf("  %s  %s  %s\n", ind, s.Ref, strings.Join(toolParts, "  "))
				} else {
					cmd.Printf("  %s  %s\n", ind, s.Ref)
				}
			}
			return nil
		},
	}
	cmd.Flags().String("market", "", "filter to market")
	cmd.Flags().Bool("short", false, "one-line summary")
	cmd.Flags().Bool("json", false, "JSON output")
	return cmd
}

func newStatusCmd(svc Services, opts *GlobalOpts) *cobra.Command {
	cmd := newCheckCmd(svc, opts)
	cmd.Use = "status"
	cmd.Short = "Show status of installed entries (alias for check)"
	return cmd
}

func newAddCmd(svc Services, opts *GlobalOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <ref>",
		Short: "Install an entry from a market",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			noDeps, _ := cmd.Flags().GetBool("no-deps")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			acceptBreaking, _ := cmd.Flags().GetBool("accept-breaking")
			jsonOut, _ := cmd.Flags().GetBool("json")
			toolsStr, _ := cmd.Flags().GetString("tools")
			ref := domain.MctRef(args[0])
			confirmMarket := func(marketURL string) bool {
				if opts.CI {
					return false
				}
				cmd.Printf("  ?  Skill dependency requires market %s. Add it? [y/N] ", marketURL)
				var answer string
				_, _ = fmt.Fscan(cmd.InOrStdin(), &answer)
				return answer == "y" || answer == "Y"
			}
			tools := parseToolsFlag(toolsStr)
			result, err := svc.Entries.Add(ref, service.AddOpts{
				NoDeps:         noDeps,
				DryRun:         dryRun,
				AcceptBreaking: acceptBreaking,
				ConfirmMarket:  confirmMarket,
				Tools:          tools,
			})
			if err != nil {
				return err
			}
			if jsonOut {
				out := map[string]any{"ref": ref, "status": "installed"}
				if len(result.ToolWrites) > 0 {
					out["tool_writes"] = result.ToolWrites
				}
				if len(result.Warnings) > 0 {
					out["warnings"] = result.Warnings
				}
				return printJSON(cmd.OutOrStdout(), out)
			}
			cmd.Printf("  ok  installed %s\n", ref)
			for tool, path := range result.ToolWrites {
				cmd.Printf("  ok  %s  %s\n", tool, path)
			}
			for _, w := range result.Warnings {
				cmd.Printf("  --  %s\n", w)
			}
			return nil
		},
	}
	cmd.Flags().Bool("no-deps", false, "skip dependency resolution")
	cmd.Flags().Bool("dry-run", false, "preview install")
	cmd.Flags().Bool("accept-breaking", false, "accept breaking changes")
	cmd.Flags().Bool("json", false, "JSON output")
	cmd.Flags().StringP("tools", "t", "", "target tools (comma-separated, e.g. opencode,cursor)")
	return cmd
}

// parseToolsFlag converts a comma-separated tools string to a map[string]bool.
func parseToolsFlag(s string) map[string]bool {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	m := make(map[string]bool, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			m[p] = true
		}
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

func newRestoreCmd(svc Services, opts *GlobalOpts) *cobra.Command {
	cmd := newImportCmd(svc, opts)
	cmd.Use = "restore"
	cmd.Short = "Restore setup from .mct.json (alias for import .mct.json)"
	cmd.Args = cobra.NoArgs
	cmd.Flags().StringP("file", "f", ".mct.json", "file to restore from")
	importRunE := cmd.RunE
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		file, _ := cmd.Flags().GetString("file")
		return importRunE(cmd, []string{file})
	}
	return cmd
}

func newSaveCmd(svc Services, opts *GlobalOpts) *cobra.Command {
	cmd := newExportCmd(svc, opts)
	cmd.Use = "save"
	cmd.Short = "Save current setup to .mct.json (alias for export .mct.json)"
	cmd.Args = cobra.NoArgs
	cmd.Flags().StringP("file", "f", ".mct.json", "file to save to")
	exportRunE := cmd.RunE
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		file, _ := cmd.Flags().GetString("file")
		return exportRunE(cmd, []string{file})
	}
	return cmd
}

func newInstallCmd(svc Services, opts *GlobalOpts) *cobra.Command {
	cmd := newAddCmd(svc, opts)
	cmd.Use = "install <ref>"
	cmd.Short = "Install an entry from a market (alias for add)"
	return cmd
}

func newRemoveCmd(svc Services, opts *GlobalOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove [ref]",
		Short: "Remove an installed entry",
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, _ := cmd.Flags().GetString("ref")
			if ref == "" && len(args) > 0 {
				ref = args[0]
			}
			all, _ := cmd.Flags().GetBool("all")
			allLocations, _ := cmd.Flags().GetBool("all-locations")
			jsonOut, _ := cmd.Flags().GetBool("json")

			if all {
				yes, _ := cmd.Flags().GetBool("yes")
				entries, err := svc.Entries.List(service.ListOpts{Installed: true})
				if err != nil {
					return err
				}
				if !yes {
					cmd.Printf("  Remove %d installed entries? [y/N] ", len(entries))
					var answer string
					_, _ = fmt.Fscan(cmd.InOrStdin(), &answer)
					if answer != "y" && answer != "Y" {
						cmd.Println("  aborted")
						return nil
					}
				}
				type removeResultItem struct {
					Ref          domain.MctRef `json:"ref"`
					Status       string        `json:"status"`
					Error        string        `json:"error,omitempty"`
					ToolsRemoved []string      `json:"tools_removed,omitempty"`
				}
				var results []removeResultItem
				for _, e := range entries {
					rr, err := svc.Entries.Remove(e.Ref, service.RemoveOpts{AllLocations: allLocations})
					if err != nil {
						results = append(results, removeResultItem{Ref: e.Ref, Status: "error", Error: err.Error()})
					} else {
						results = append(results, removeResultItem{Ref: e.Ref, Status: "removed", ToolsRemoved: rr.ToolsRemoved})
					}
				}
				if jsonOut {
					return printJSON(cmd.OutOrStdout(), results)
				}
				for _, r := range results {
					if r.Error != "" {
						cmd.PrintErrf("  x  %s: %s\n", r.Ref, r.Error)
					} else {
						tools := strings.Join(r.ToolsRemoved, ", ")
						if tools != "" {
							cmd.Printf("  ok  removed %s [%s]\n", r.Ref, tools)
						} else {
							cmd.Printf("  ok  removed %s\n", r.Ref)
						}
					}
				}
				return nil
			}

			rr, err := svc.Entries.Remove(domain.MctRef(ref), service.RemoveOpts{AllLocations: allLocations})
			if err != nil {
				return err
			}
			if jsonOut {
				out := map[string]any{"ref": ref, "status": "removed"}
				if len(rr.ToolsRemoved) > 0 {
					out["tools_removed"] = rr.ToolsRemoved
				}
				return printJSON(cmd.OutOrStdout(), out)
			}
			tools := strings.Join(rr.ToolsRemoved, ", ")
			if tools != "" {
				cmd.Printf("  ok  removed %s [%s]\n", ref, tools)
			} else {
				cmd.Printf("  ok  removed %s\n", ref)
			}
			return nil
		},
	}
	cmd.Flags().String("ref", "", "entry ref to remove")
	cmd.Flags().Bool("all", false, "remove all installed entries")
	cmd.Flags().Bool("all-locations", false, "remove from all locations, not just current project")
	cmd.Flags().Bool("yes", false, "skip confirmation prompt")
	cmd.Flags().Bool("json", false, "JSON output")
	return cmd
}

func newPruneCmd(svc Services, opts *GlobalOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prune",
		Short: "Process deleted entries",
		RunE: func(cmd *cobra.Command, args []string) error {
			ref, _ := cmd.Flags().GetString("ref")
			allKeep, _ := cmd.Flags().GetBool("all-keep")
			allRemove, _ := cmd.Flags().GetBool("all-remove")
			jsonOut, _ := cmd.Flags().GetBool("json")
			results, err := svc.Entries.Prune(service.PruneOpts{
				Ref:       domain.MctRef(ref),
				AllKeep:   allKeep,
				AllRemove: allRemove,
			})
			if err != nil {
				return err
			}
			if jsonOut {
				return printJSON(cmd.OutOrStdout(), results)
			}
			for _, r := range results {
				cmd.Printf("  %s  %s\n", r.Action, r.Ref)
			}
			return nil
		},
	}
	cmd.Flags().String("ref", "", "specific entry ref")
	cmd.Flags().Bool("all-keep", false, "keep all deleted entries")
	cmd.Flags().Bool("all-remove", false, "remove all deleted entries")
	cmd.Flags().Bool("json", false, "JSON output")
	return cmd
}

func newSearchCmd(svc Services, opts *GlobalOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search entries across all markets",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			limit, _ := cmd.Flags().GetInt("limit")
			market, _ := cmd.Flags().GetString("market")
			entryType, _ := cmd.Flags().GetString("type")
			category, _ := cmd.Flags().GetString("category")
			installed, _ := cmd.Flags().GetBool("installed")
			notInstalled, _ := cmd.Flags().GetBool("not-installed")
			includeDeleted, _ := cmd.Flags().GetBool("include-deleted")
			jsonOut, _ := cmd.Flags().GetBool("json")

			results, err := svc.Search.Search(args[0], service.SearchOpts{
				Type:           domain.EntryType(entryType),
				Market:         market,
				Category:       category,
				Installed:      installed,
				NotInstalled:   notInstalled,
				IncludeDeleted: includeDeleted,
				Limit:          limit,
				JSON:           jsonOut,
			})
			if err != nil {
				return err
			}

			if jsonOut {
				return printJSON(cmd.OutOrStdout(), results)
			}

			type profileKey struct{ market, category string }
			type profileInfo struct {
				market    string
				category  string
				desc      string
				tags      []string
				score     float64
				installed bool
			}
			seen := make(map[profileKey]*profileInfo)
			var order []profileKey
			for _, r := range results {
				key := profileKey{r.Entry.Market, r.Entry.Category}
				if _, ok := seen[key]; !ok {
					desc := r.Entry.ProfileDescription
					if desc == "" {
						desc = r.Entry.Description
					}
					seen[key] = &profileInfo{
						market:   r.Entry.Market,
						category: r.Entry.Category,
						desc:     desc,
						tags:     r.Entry.MctTags,
						score:    r.Score,
					}
					order = append(order, key)
				}
				p := seen[key]
				if r.Entry.Installed {
					p.installed = true
				}
				if r.Score > p.score {
					p.score = r.Score
				}
			}

			cmd.Printf("\n  %d results\n\n", len(order))
			for i, key := range order {
				p := seen[key]
				indicator := " "
				if p.installed {
					indicator = "ok"
				}
				cmd.Printf("  %d  %s@%s  %s  score: %.2f\n",
					i+1, p.market, p.category, indicator, p.score)
				if p.desc != "" {
					cmd.Printf("     %s\n", p.desc)
				}
				if len(p.tags) > 0 {
					cmd.Printf("     Tags: %s\n", strings.Join(p.tags, ", "))
				}
				cmd.Printf("     mct add %s@%s\n", p.market, p.category)
				cmd.Println()
			}
			return nil
		},
	}
	cmd.Flags().Int("limit", 10, "max results")
	cmd.Flags().String("type", "", "filter by type (agent|skill|command|hook)")
	cmd.Flags().String("market", "", "filter by market")
	cmd.Flags().String("category", "", "filter by category")
	cmd.Flags().Bool("installed", false, "only installed")
	cmd.Flags().Bool("not-installed", false, "only not installed")
	cmd.Flags().Bool("include-deleted", false, "include deleted entries")
	cmd.Flags().Bool("json", false, "JSON output")
	return cmd
}

func newListCmd(svc Services, opts *GlobalOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List locally installed profiles",
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonOut, _ := cmd.Flags().GetBool("json")
			entries, err := svc.Entries.List(service.ListOpts{Installed: true})
			if err != nil {
				return err
			}

			// Determine enabled tools from config
			var enabledTools []string
			cfg, cfgErr := svc.Config.GetConfig()
			if cfgErr == nil {
				enabledTools = append(enabledTools, "claude") // always present
				for tool, enabled := range cfg.Tools {
					if enabled && tool != "claude" {
						enabledTools = append(enabledTools, tool)
					}
				}
			}

			type profileKey struct{ market, profile string }
			type profileInfo struct {
				Market  string          `json:"market"`
				Profile string          `json:"profile"`
				Agents  int             `json:"agents"`
				Skills  int             `json:"skills"`
				Tools   []string        `json:"tools,omitempty"`
				Refs    []domain.MctRef `json:"refs,omitempty"`
			}
			seen := make(map[profileKey]*profileInfo)
			var order []profileKey
			for _, e := range entries {
				_, relPath, _ := e.Ref.Parse()
				parts := strings.SplitN(relPath, "/", 3)
				profile := relPath
				if len(parts) >= 2 {
					profile = parts[0] + "/" + parts[1]
				}
				key := profileKey{e.Market, profile}
				if _, ok := seen[key]; !ok {
					seen[key] = &profileInfo{Market: e.Market, Profile: profile, Tools: enabledTools}
					order = append(order, key)
				}
				if e.Type == domain.EntryTypeAgent {
					seen[key].Agents++
				} else {
					seen[key].Skills++
				}
				seen[key].Refs = append(seen[key].Refs, e.Ref)
			}

			if jsonOut {
				result := make([]profileInfo, 0, len(order))
				for _, k := range order {
					result = append(result, *seen[k])
				}
				return printJSON(cmd.OutOrStdout(), result)
			}
			for _, k := range order {
				p := seen[k]
				toolLabel := ""
				if len(p.Tools) > 0 {
					toolLabel = fmt.Sprintf("  [%s]", strings.Join(p.Tools, ", "))
				}
				cmd.Printf("  %s@%s  (%d agents, %d skills)%s\n", p.Market, p.Profile, p.Agents, p.Skills, toolLabel)
				for _, e := range p.Refs {
					cmd.Printf("    %s\n", e)
				}
			}
			return nil
		},
	}
	cmd.Flags().Bool("json", false, "JSON output")
	return cmd
}

func newMarketsCmd(svc Services, opts *GlobalOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "markets",
		Short: "List configured markets (alias for market list)",
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonOut, _ := cmd.Flags().GetBool("json")
			markets, err := svc.Markets.ListMarkets()
			if err != nil {
				return err
			}
			if jsonOut {
				return printJSON(cmd.OutOrStdout(), markets)
			}
			for _, m := range markets {
				status := "rw"
				if m.ReadOnly {
					status = "ro"
				}
				cmd.Printf("  %s  %-20s  %s\n", status, m.Name, m.URL)
			}
			return nil
		},
	}
	cmd.Flags().Bool("json", false, "JSON output")
	return cmd
}

func newConflictsCmd(svc Services, opts *GlobalOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "conflicts",
		Short: "Show all conflicts",
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonOut, _ := cmd.Flags().GetBool("json")
			conflicts, err := svc.Entries.Conflicts()
			if err != nil {
				return err
			}
			if jsonOut {
				return printJSON(cmd.OutOrStdout(), conflicts)
			}
			if len(conflicts) == 0 {
				cmd.Println("  No conflicts")
				return nil
			}
			for _, c := range conflicts {
				cmd.Printf("  %s [%s]  %s\n", c.Severity, c.Type, c.Description)
			}
			return nil
		},
	}
	cmd.Flags().Bool("json", false, "JSON output")
	return cmd
}

func newSyncStateCmd(svc Services, opts *GlobalOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync-state",
		Short: "Print sync state",
		RunE: func(cmd *cobra.Command, args []string) error {
			jsonOut, _ := cmd.Flags().GetBool("json")
			state, err := svc.Sync.SyncState()
			if err != nil {
				return err
			}
			if jsonOut {
				return printJSON(cmd.OutOrStdout(), state)
			}
			for name, ms := range state.Markets {
				cmd.Printf("  %s: %s (%s) synced %s\n", name, ms.LastSyncedSHA[:7], ms.Status, ms.LastSyncedAt.Format("2006-01-02 15:04"))
			}
			return nil
		},
	}
	cmd.Flags().Bool("json", false, "JSON output")
	return cmd
}

func newIndexCmd(svc Services, opts *GlobalOpts) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "index",
		Short: "Index operations",
		RunE: func(cmd *cobra.Command, args []string) error {
			bench, _ := cmd.Flags().GetBool("bench")
			dump, _ := cmd.Flags().GetBool("dump")

			if bench {
				r, err := svc.Search.BenchIndex()
				if err != nil {
					return err
				}
				cmd.Printf("  entries:  %d\n", r.Entries)
				cmd.Printf("  vocab:    %d terms\n", r.Vocab)
				cmd.Printf("  scan:     %s\n", r.Scan)
				cmd.Printf("  index:    %s\n", r.Index)
				cmd.Printf("  total:    %s\n", r.Total)
				return nil
			}

			if dump {
				entries, err := svc.Search.DumpIndex()
				if err != nil {
					return err
				}
				data, _ := json.MarshalIndent(entries, "", "  ")
				cmd.Println(string(data))
				return nil
			}

			return fmt.Errorf("use --bench or --dump")
		},
	}
	cmd.Flags().Bool("dump", false, "dump index as JSON")
	cmd.Flags().Bool("bench", false, "measure indexing time")
	return cmd
}

func newInitCmd(svc Services, opts *GlobalOpts) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize mct in current project",
		RunE: func(cmd *cobra.Command, args []string) error {
			return svc.Entries.Init(service.InitOpts{
				LocalPath: ".claude/",
				CI:        opts.CI,
			})
		},
	}
}
