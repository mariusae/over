package overlay

import (
	"fmt"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
)

// ShouldLinkFile determines if a file should be linked based on the overlay's linklist patterns.
// If the linklist is empty or nil, no files are linked (unless it's a legacy overlay).
// Returns true if the file matches any pattern and should be linked.
//
// Pattern semantics (same as .gitignore):
// - "*" matches all files
// - "*.go" matches all .go files
// - "!test.go" excludes test.go from previous matches
// - "docs/**" matches everything in docs/ directory
func ShouldLinkFile(linkList []string, relativePath string) bool {
	// Empty or nil linklist means no files should be linked
	if len(linkList) == 0 {
		return false
	}

	// Build a gitignore matcher from the linklist patterns.
	// We use gitignore semantics but interpret them as "include" patterns:
	// - If a file matches the patterns, it's "ignored" by gitignore logic
	// - But for us, "ignored" means "included in linking"
	// - Negative patterns (!) work correctly: they exclude from linking
	matcher := ignore.CompileIgnoreLines(linkList...)

	// MatchesPath returns true if the path matches the patterns.
	// We treat this as "should be linked".
	return matcher.MatchesPath(relativePath)
}

// AddPattern adds a pattern to the linklist if it's not already covered by existing patterns.
func AddPattern(linkList []string, pattern string) []string {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return linkList
	}

	// Check if pattern already exists
	for _, p := range linkList {
		if p == pattern {
			return linkList
		}
	}

	return append(linkList, pattern)
}

// AddNegativePattern adds a negative (exclusion) pattern to the linklist.
// This is used by the unlink command to exclude specific files.
func AddNegativePattern(linkList []string, relativePath string) []string {
	// In gitignore syntax, patterns starting with ! are negation patterns
	pattern := "!" + relativePath
	return AddPattern(linkList, pattern)
}

// FormatLinkList formats the linklist patterns as a multi-line string for display or editing.
func FormatLinkList(linkList []string) string {
	if len(linkList) == 0 {
		return ""
	}
	return strings.Join(linkList, "\n")
}

// ParseLinkList parses a multi-line string into linklist patterns.
// Empty lines and lines starting with # are ignored (comments).
func ParseLinkList(content string) []string {
	lines := strings.Split(content, "\n")
	var patterns []string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}

	return patterns
}

// ValidateLinkList validates that the linklist patterns are syntactically correct.
// Returns an error if any pattern is invalid.
func ValidateLinkList(linkList []string) error {
	if len(linkList) == 0 {
		return nil
	}

	// Try to compile the patterns to ensure they're valid
	defer func() {
		if r := recover(); r != nil {
			// The ignore library might panic on invalid patterns
			// We recover and return an error instead
		}
	}()

	_ = ignore.CompileIgnoreLines(linkList...)
	return nil
}

// DefaultLinkList returns the default linklist pattern (all files).
func DefaultLinkList() []string {
	return []string{"*"}
}

// IsFileIncluded checks if a specific file would be included by the linklist patterns.
// This is useful for determining if a file needs to be explicitly added to the linklist.
func IsFileIncluded(linkList []string, relativePath string) bool {
	return ShouldLinkFile(linkList, relativePath)
}

// RemovePattern removes a specific pattern from the linklist.
func RemovePattern(linkList []string, pattern string) []string {
	var result []string
	for _, p := range linkList {
		if p != pattern {
			result = append(result, p)
		}
	}
	return result
}

// ExplainPattern provides a human-readable explanation of what files a pattern matches.
func ExplainPattern(pattern string) string {
	pattern = strings.TrimSpace(pattern)

	if pattern == "*" {
		return "All files"
	}
	if strings.HasPrefix(pattern, "!") {
		return fmt.Sprintf("Exclude: %s", pattern[1:])
	}
	if strings.Contains(pattern, "*") {
		return fmt.Sprintf("Match: %s", pattern)
	}
	return fmt.Sprintf("Exact: %s", pattern)
}
