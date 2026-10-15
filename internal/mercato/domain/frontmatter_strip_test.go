package domain_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/JLugagne/agents-mercato/internal/mercato/domain"
)

func TestStripRequiresSkills_KeepsValidYAML(t *testing.T) {
	cases := map[string]string{
		"last key":   "---\ndescription: x\nrequires_skills:\n  - file: skills/a/SKILL.md\n  - file: skills/b/SKILL.md\n    market: m\n---\nbody\n",
		"middle key": "---\ndescription: x\nrequires_skills:\n  - file: skills/a/SKILL.md\ntools: Read\n---\nbody\n",
		"first key":  "---\nrequires_skills:\n- file: skills/a/SKILL.md\ndescription: x\n---\nbody\n",
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			out := domain.StripRequiresSkills([]byte(in))
			fm, err := domain.ExtractFrontmatterBytes(out)
			require.NoError(t, err)
			var m map[string]any
			require.NoError(t, yaml.Unmarshal(fm, &m), string(out))
			require.NotContains(t, m, "requires_skills")
			require.Equal(t, "x", m["description"])
			require.NotContains(t, string(out), "file:")
			require.Contains(t, string(out), "body\n")
		})
	}
	t.Run("middle key keeps following key", func(t *testing.T) {
		out := domain.StripRequiresSkills([]byte(cases["middle key"]))
		require.Contains(t, string(out), "tools: Read")
	})
}
