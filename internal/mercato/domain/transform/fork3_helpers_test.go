package transform_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

func f3Input(t domain.EntryType, rel, content string) domain.TransformInput {
	return domain.TransformInput{
		Entry:   domain.Entry{Type: t, RelPath: rel, Filename: rel},
		Content: []byte(content),
		Exists:  func(string) bool { return false },
	}
}

func f3Hook(event, matcher string, timeout int) string {
	h := map[string]any{"type": "command", "command": "echo hi"}
	if timeout > 0 {
		h["timeout"] = timeout
	}
	b, _ := json.Marshal(map[string]any{"event": event, "matcher": matcher, "hooks": []any{h}})
	return string(b)
}

func f3Frontmatter(t *testing.T, content []byte) map[string]any {
	t.Helper()
	s := string(content)
	require.True(t, len(s) > 4 && s[:4] == "---\n")
	end := len(s)
	for i := 4; i+4 <= len(s); i++ {
		if s[i:i+4] == "---\n" && s[i-1] == '\n' {
			end = i
			break
		}
	}
	var m map[string]any
	require.NoError(t, yaml.Unmarshal([]byte(s[4:end]), &m))
	return m
}
