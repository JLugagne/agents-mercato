package transform_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

func f1Input(t domain.EntryType, rel, content string) domain.TransformInput {
	return domain.TransformInput{
		Entry:   domain.Entry{Type: t, RelPath: rel, Filename: rel[strings.LastIndex(rel, "/")+1:]},
		Content: []byte(content),
		Exists:  func(string) bool { return false },
	}
}

func f1Hook(content string) domain.TransformInput {
	return f1Input(domain.EntryTypeHook, "dev/hooks/go-vet.json", content)
}

func f1Split(t *testing.T, content []byte) (map[string]any, string) {
	t.Helper()
	s := string(content)
	require.True(t, strings.HasPrefix(s, "---\n"), s)
	end := strings.Index(s[4:], "---\n")
	require.GreaterOrEqual(t, end, 0)
	var m map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(s[4:4+end]), &m))
	return m, s[4+end+4:]
}

const (
	f1Agent   = "---\nname: reviewer\ndescription: 'Reviews: Go code'\nmodel: opus\ntools: Read, Grep, Bash, Edit, mcp__gh__pr\n---\nYou review.\n"
	f1Command = "---\ndescription: Fix an issue\nargument-hint: <issue>\nmodel: haiku\nallowed-tools: Bash\n---\nFix $ARGUMENTS then $1.\n"
	f1Skill   = "---\nname: other\ndescription: Go style\n---\nUse gofmt.\n"
)
