package skills

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// expectedSkills is the set of skills atlas-ai ships. A skill missing from
// the embedded FS is a packaging error, not a neutrality one.
var expectedSkills = []string{"checked-loop", "session-end", "session-start"}

// forbiddenTokens are strings that tie a skill to one agent or one machine.
// A skill is copied unchanged to every supported agent, so none may appear.
//
// This list is public and generic. Names that identify a person (private
// project names, hosts, machine-specific paths) are never committed: a
// maintainer keeps them in the optional file localTokensFile, which the test
// adds to this list when it exists.
var forbiddenTokens = []string{
	// Agent-specific logbook values, files, tool prefixes and tool names.
	"--tool pi",
	"--tool 'Claude CLI'",
	"APPEND_SYSTEM.md",
	"gateway-mcp_",
	"engram_mem_",
	"cbm_",
	"mcp__",
	"pi-subagents",
	"atlas_search",

	// Absolute paths under a mount point or a home directory.
	"/mnt/",
	"/home/",
}

// localTokensFile is an optional, git-ignored list of extra forbidden tokens,
// one per line, read from the package directory. It is a regression guard for
// personal residue already found once, not a general detector of personal
// content: new names still need a human read of the diff.
const localTokensFile = "forbidden.local.txt"

const (
	maxNameLen        = 64
	maxDescriptionLen = 1024
)

var validName = regexp.MustCompile(`^[a-z0-9-]+$`)

type frontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// tokenHit is one forbidden token found on one line (1-based).
type tokenHit struct {
	Line  int
	Token string
}

// splitFrontmatter returns the YAML header of a SKILL.md file.
func splitFrontmatter(content string) (string, error) {
	const fence = "---\n"
	if !strings.HasPrefix(content, fence) {
		return "", fmt.Errorf("file does not start with a %q frontmatter fence", "---")
	}
	rest := content[len(fence):]
	end := strings.Index(rest, "\n"+fence)
	if end < 0 {
		return "", fmt.Errorf("frontmatter is not closed by a %q line", "---")
	}
	return rest[:end+1], nil
}

// parseTokenList returns the tokens of a list file: one per line, surrounding
// whitespace trimmed, blank lines and lines starting with "#" ignored.
func parseTokenList(content string) []string {
	var tokens []string
	for _, line := range strings.Split(content, "\n") {
		token := strings.TrimSpace(line)
		if token == "" || strings.HasPrefix(token, "#") {
			continue
		}
		tokens = append(tokens, token)
	}
	return tokens
}

// localTokens returns the tokens of localTokensFile, or nil when the file
// does not exist. Any other read error fails the test.
func localTokens(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(localTokensFile)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("read %s: %v", localTokensFile, err)
	}
	return parseTokenList(string(raw))
}

// forbiddenTokenHits returns every one of tokens found in content, in line
// order and, within a line, in tokens order.
func forbiddenTokenHits(content string, tokens []string) []tokenHit {
	var hits []tokenHit
	for i, line := range strings.Split(content, "\n") {
		for _, token := range tokens {
			if strings.Contains(line, token) {
				hits = append(hits, tokenHit{Line: i + 1, Token: token})
			}
		}
	}
	return hits
}

// describeHit returns the failure message for one hit. A token from the local
// list is reported by its 1-based position in that list, never by its text, so
// test output can be shared without publishing a private name. A token that is
// in both lists is treated as local.
func describeHit(hit tokenHit, local []string) string {
	for i, token := range local {
		if token == hit.Token {
			return fmt.Sprintf("line %d contains local forbidden token #%d (see %s)", hit.Line, i+1, localTokensFile)
		}
	}
	return fmt.Sprintf("line %d contains agent- or machine-specific token %q", hit.Line, hit.Token)
}

func readSkill(t *testing.T, dir string) string {
	t.Helper()
	raw, err := fs.ReadFile(FS, dir+"/SKILL.md")
	if err != nil {
		t.Fatalf("read %s/SKILL.md: %v", dir, err)
	}
	return strings.ReplaceAll(string(raw), "\r\n", "\n")
}

func skillDirs(t *testing.T) []string {
	t.Helper()
	entries, err := fs.ReadDir(FS, ".")
	if err != nil {
		t.Fatalf("read embedded skills: %v", err)
	}
	var dirs []string
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry.Name())
		}
	}
	return dirs
}

func TestSplitFrontmatter(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
		wantErr bool
	}{
		{
			name:    "valid header",
			content: "---\nname: demo\ndescription: A demo skill.\n---\n\n## Body\n",
			want:    "name: demo\ndescription: A demo skill.\n",
		},
		{
			name:    "missing opening fence",
			content: "name: demo\n---\n\n## Body\n",
			wantErr: true,
		},
		{
			name:    "unclosed frontmatter",
			content: "---\nname: demo\ndescription: A demo skill.\n\n## Body\n",
			wantErr: true,
		},
		{
			name:    "empty file",
			content: "",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := splitFrontmatter(tc.content)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got header %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Errorf("header = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestForbiddenTokenHits(t *testing.T) {
	// An explicit list keeps this test independent of the public list and of
	// any local file.
	tokens := []string{"--tool pi", "cbm_", "/mnt/", "example-project"}

	cases := []struct {
		name    string
		content string
		want    []tokenHit
	}{
		{
			name:    "clean text",
			content: "Call the Engram memory tools.\nRead the vault at $ATLAS_VAULT.\n",
			want:    nil,
		},
		{
			name:    "one hit reports its line",
			content: "First line.\nRun logbook new --tool pi now.\nLast line.\n",
			want:    []tokenHit{{Line: 2, Token: "--tool pi"}},
		},
		{
			name:    "several tokens on one line",
			content: "Use cbm_index_status under /mnt/data/vault.\n",
			want:    []tokenHit{{Line: 1, Token: "cbm_"}, {Line: 1, Token: "/mnt/"}},
		},
		{
			name:    "token from the end of the list",
			content: "ok\nok\nAs seen in the example-project-7 run.\n",
			want:    []tokenHit{{Line: 3, Token: "example-project"}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := forbiddenTokenHits(tc.content, tokens)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("hits = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseTokenList(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    []string
	}{
		{
			name:    "empty content",
			content: "",
			want:    nil,
		},
		{
			name:    "only comments and blank lines",
			content: "# a comment\n\n   \n# another\n",
			want:    nil,
		},
		{
			name:    "plain lines keep their order",
			content: "first\nsecond\nthird\n",
			want:    []string{"first", "second", "third"},
		},
		{
			name:    "comments and blank lines are skipped",
			content: "# header\nfirst\n\n# note\nsecond\n",
			want:    []string{"first", "second"},
		},
		{
			name:    "surrounding whitespace and CRLF are trimmed",
			content: "  first  \r\n\tsecond\r\n",
			want:    []string{"first", "second"},
		},
		{
			name:    "no trailing newline",
			content: "first\nsecond",
			want:    []string{"first", "second"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseTokenList(tc.content)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("tokens = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDescribeHit(t *testing.T) {
	local := []string{"example-project", "example-host", "cbm_"}

	cases := []struct {
		name       string
		hit        tokenHit
		want       string
		mustNotSay string
	}{
		{
			name: "public token is quoted",
			hit:  tokenHit{Line: 4, Token: "--tool pi"},
			want: `line 4 contains agent- or machine-specific token "--tool pi"`,
		},
		{
			name:       "local token is replaced by its number",
			hit:        tokenHit{Line: 9, Token: "example-host"},
			want:       "line 9 contains local forbidden token #2 (see forbidden.local.txt)",
			mustNotSay: "example-host",
		},
		{
			name:       "token in both lists is treated as local",
			hit:        tokenHit{Line: 1, Token: "cbm_"},
			want:       "line 1 contains local forbidden token #3 (see forbidden.local.txt)",
			mustNotSay: "cbm_",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := describeHit(tc.hit, local)
			if got != tc.want {
				t.Errorf("message = %q, want %q", got, tc.want)
			}
			if tc.mustNotSay != "" && strings.Contains(got, tc.mustNotSay) {
				t.Errorf("message %q must not contain the local token", got)
			}
		})
	}
}

func TestExpectedSkillsAreShipped(t *testing.T) {
	shipped := map[string]bool{}
	for _, dir := range skillDirs(t) {
		shipped[dir] = true
	}
	for _, name := range expectedSkills {
		if !shipped[name] {
			t.Errorf("skill %q is not embedded", name)
		}
	}
}

func TestFrontmatterIsValidForEveryAgent(t *testing.T) {
	for _, dir := range skillDirs(t) {
		t.Run(dir, func(t *testing.T) {
			header, err := splitFrontmatter(readSkill(t, dir))
			if err != nil {
				t.Fatal(err)
			}
			var meta frontmatter
			if err := yaml.Unmarshal([]byte(header), &meta); err != nil {
				t.Fatalf("parse frontmatter: %v", err)
			}

			if meta.Name != dir {
				t.Errorf("name %q must equal its directory %q", meta.Name, dir)
			}
			if !validName.MatchString(meta.Name) {
				t.Errorf("name %q must contain only lowercase letters, digits and hyphens", meta.Name)
			}
			if n := utf8.RuneCountInString(meta.Name); n > maxNameLen {
				t.Errorf("name is %d characters, maximum is %d", n, maxNameLen)
			}

			description := strings.TrimSpace(meta.Description)
			if description == "" {
				t.Error("description must not be empty")
			}
			if n := utf8.RuneCountInString(description); n > maxDescriptionLen {
				t.Errorf("description is %d characters, maximum is %d", n, maxDescriptionLen)
			}
		})
	}
}

func TestSkillsAreAgentNeutral(t *testing.T) {
	local := localTokens(t)
	// The local tokens stay private: only their count is logged here, and a
	// failure reports a local token by its number (see describeHit).
	t.Logf("%d local tokens loaded from %s", len(local), localTokensFile)
	tokens := append(append([]string{}, forbiddenTokens...), local...)

	for _, dir := range skillDirs(t) {
		t.Run(dir, func(t *testing.T) {
			for _, hit := range forbiddenTokenHits(readSkill(t, dir), tokens) {
				t.Error(describeHit(hit, local))
			}
		})
	}
}
