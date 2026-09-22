package session

import (
	"strings"
	"time"

	"github.com/lk16/box/internal/config"
)

// BranchNameTimeout is the one turn a branch name is worth, since naming one is a courtesy.
const BranchNameTimeout = 10 * time.Second

// ToBranchName turns an agent's answer into a branch name: its last line, kebab-cased and cut short.
func ToBranchName(text string) string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	words := strings.Split(config.ToKebabCase(lines[len(lines)-1]), "-")
	return strings.Trim(strings.Join(words[:min(len(words), config.BranchNameWords)], "-"), "-")
}

// BranchNameCommand assembles the headless Claude invocation that names a branch after the work.
func BranchNameCommand(subjects string) []string {
	return []string{"claude", "-p", config.BranchNamePrompt + "\n" + subjects}
}

// SuggestBranchName asks Claude for a branch name, returning nothing when it fails or stalls.
func (s Session) SuggestBranchName(subjects string) string {
	result := s.Deps.Run.Timed(BranchNameCommand(subjects), BranchNameTimeout)
	if result.Code != 0 {
		return ""
	}
	return ToBranchName(result.Stdout)
}
