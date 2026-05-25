package gitadapter

import "testing"

func TestIsAgentOrSkillPath(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"agents/foo.md", true},
		{"dev/go/agents/bar.md", true},
		{"skills/bar/SKILL.md", true},
		{"dev/go/skills/baz/SKILL.md", true},
		{"commands/refactor.md", true},
		{"dev/go/commands/refactor.md", true},
		{"hooks/go-vet.json", true},
		{"dev/go/hooks/go-vet.json", true},
		{"README.md", false},
		{"dev/go/README.md", false},
		{"assets/icon.png", false},
		{"some/random/file.txt", false},
	}
	for _, tc := range cases {
		got := isAgentOrSkillPath(tc.path)
		if got != tc.want {
			t.Errorf("isAgentOrSkillPath(%q) = %v, want %v", tc.path, got, tc.want)
		}
	}
}
