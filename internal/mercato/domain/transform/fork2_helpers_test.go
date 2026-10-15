package transform_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

func f2Input(t domain.EntryType, rel, content string) domain.TransformInput {
	return domain.TransformInput{
		Entry:   domain.Entry{Type: t, RelPath: rel, Filename: rel},
		Content: []byte(content),
		Exists:  func(string) bool { return false },
	}
}

func f2Hook(event, matcher, timeout string) domain.TransformInput {
	m := ""
	if matcher != "" {
		m = `"matcher":"` + matcher + `",`
	}
	to := ""
	if timeout != "" {
		to = `,"timeout":` + timeout
	}
	return f2Input(domain.EntryTypeHook, "dev/hooks/Lint_Hook.json",
		`{"event":"`+event+`",`+m+`"hooks":[{"type":"command","command":"go vet ./..."`+to+`}]}`)
}

func f2Frontmatter(t *testing.T, content []byte) map[string]any {
	t.Helper()
	s := string(content)
	require.Equal(t, "---\n", s[:4])
	end := len(s)
	for i := 4; i+4 <= len(s); i++ {
		if s[i:i+4] == "\n---" {
			end = i
			break
		}
	}
	var m map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(s[4:end]), &m))
	return m
}

func f2Entry(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	return m
}

const f2Agent = "---\nname: Code Reviewer\ndescription: Reviews code\nmodel: sonnet\ntools: Read, Bash, Edit, MultiEdit, Foo\n---\nYou review.\n"
const f2Command = "---\ndescription: Fix it\nargument-hint: <issue>\n---\nFix $ARGUMENTS now, then $1.\n"
const f2Skill = "---\nname: x\ndescription: d\n---\nUse.\n"
