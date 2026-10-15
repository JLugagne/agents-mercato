package commands

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain/service"
)

func TestMigrateCmd_PassesFlagsAndPrintsActions(t *testing.T) {
	var got service.MigrateOpts
	svc := mockServices()
	svc.Sync = &stubSync{migrateFn: func(opts service.MigrateOpts) ([]service.MigrateResult, error) {
		got = opts
		return []service.MigrateResult{
			{Market: "mkt", Profile: "dev/go", Location: "/p", Actions: []string{"removed .cursor/rules/x.mdc"}, Warnings: []string{"kept .github/copilot-instructions.md"}},
		}, nil
	}}

	out, err := runCmd(t, svc, "migrate", "--dry-run", "--all-locations")

	require.NoError(t, err)
	require.Equal(t, service.MigrateOpts{AllLocations: true, DryRun: true}, got)
	require.Contains(t, out, "mkt@dev/go")
	require.Contains(t, out, "removed .cursor/rules/x.mdc")
	require.Contains(t, out, "kept .github/copilot-instructions.md")
}

func TestMigrateCmd_NothingToDo(t *testing.T) {
	svc := mockServices()
	svc.Sync = &stubSync{}

	out, err := runCmd(t, svc, "migrate")

	require.NoError(t, err)
	require.Contains(t, out, "nothing to migrate")
}

func TestUpdateCmd_PrintsMigrationDetails(t *testing.T) {
	svc := mockServices()
	svc.Sync = &stubSync{updateFn: func(opts service.UpdateOpts) ([]service.UpdateResult, error) {
		return []service.UpdateResult{{Ref: "mkt@dev/go", Location: "/p", Action: "migrated", Details: []string{"generated .opencode/agents/a.md"}}}, nil
	}}

	out, err := runCmd(t, svc, "update")

	require.NoError(t, err)
	require.Contains(t, out, "migrated")
	require.Contains(t, out, "generated .opencode/agents/a.md")
}
