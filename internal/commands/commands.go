// Package commands loads custom slash commands: Markdown files whose body
// is a prompt, from the user's and the project's command directories, plus
// the bundled ones. A command runs as a turn; its frontmatter can choose
// the agent, the model, the permitted tools and plan mode.
package commands

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed builtin/*.md
var builtinFS embed.FS

// Command is one slash command.
type Command struct {
	Name         string // without the slash; may be namespaced, "db:migrate"
	Description  string
	ArgumentHint string
	// Agent and Model run the command's turn as another agent or on another
	// model ("" for the session's).
	Agent string
	Model string
	// AllowedTools limits the turn to these tools (empty: no limit).
	AllowedTools []string
	// Plan runs the command as a plan: tools that change anything are
	// refused.
	Plan bool
	Body string
	// Source is where it came from: "bundled", "user", "project", or
	// "skill".
	Source string
	Path   string
}

type frontmatter struct {
	Description  string `yaml:"description"`
	ArgumentHint string `yaml:"argument-hint"`
	Agent        string `yaml:"agent"`
	Model        string `yaml:"model"`
	AllowedTools any    `yaml:"allowed-tools"`
	Mode         string `yaml:"mode"`
}

// validName is a command name: letters, digits, "-", "_", "." and ":"
// between namespace parts.
var validName = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*(:[a-z0-9][a-z0-9._-]*)*$`)

// ValidName reports whether name can be a command's.
func ValidName(name string) bool { return validName.MatchString(name) }

// Parse reads a command file's frontmatter (optional) and body.
func Parse(name, source, path string, data []byte) (Command, error) {
	c := Command{Name: name, Source: source, Path: path}
	text := strings.TrimPrefix(string(data), "\uFEFF") // a byte-order mark
	body := text
	if t := strings.TrimLeft(text, " \t\r\n"); strings.HasPrefix(t, "---") {
		end := strings.Index(t[3:], "\n---")
		if end < 0 {
			return c, errors.New("missing the --- line that ends the frontmatter")
		}
		var fm frontmatter
		if err := yaml.Unmarshal([]byte(t[3:3+end]), &fm); err != nil {
			return c, fmt.Errorf("frontmatter: %w", err)
		}
		rest := t[3+end+4:]
		if i := strings.IndexByte(rest, '\n'); i >= 0 {
			rest = rest[i+1:]
		} else {
			rest = ""
		}
		body = rest
		c.Description, c.ArgumentHint, c.Agent, c.Model = fm.Description, fm.ArgumentHint, fm.Agent, fm.Model
		c.AllowedTools = toolList(fm.AllowedTools)
		switch strings.ToLower(strings.TrimSpace(fm.Mode)) {
		case "", "default":
		case "plan":
			c.Plan = true
		default:
			return c, fmt.Errorf("mode %q: only plan is supported", fm.Mode)
		}
	}
	c.Body = strings.TrimSpace(body)
	if c.Body == "" {
		return c, errors.New("the command has no prompt")
	}
	if c.Description == "" {
		c.Description = firstLine(c.Body)
	}
	return c, nil
}

// toolList reads allowed-tools as a list or a space/comma-separated string.
func toolList(v any) []string {
	var out []string
	switch t := v.(type) {
	case string:
		// Split on commas and spaces outside parentheses: "Read, Bash(git *)".
		depth, start := 0, 0
		flush := func(end int) {
			if w := strings.TrimSpace(t[start:end]); w != "" {
				out = append(out, w)
			}
		}
		for i, r := range t {
			switch {
			case r == '(':
				depth++
			case r == ')' && depth > 0:
				depth--
			case (r == ',' || r == ' ') && depth == 0:
				flush(i)
				start = i + 1
			}
		}
		flush(len(t))
	case []any:
		for _, x := range t {
			if s, ok := x.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
	}
	return out
}

func firstLine(s string) string {
	for line := range strings.Lines(s) {
		if t := strings.TrimSpace(strings.TrimLeft(line, "# ")); t != "" {
			if len(t) > 80 {
				t = t[:79] + "…"
			}
			return t
		}
	}
	return ""
}

// Expand returns the command's prompt for args: $ARGUMENTS is the whole
// argument string, $1…$9 the words. Without $ARGUMENTS or $1, arguments
// are appended.
func (c Command) Expand(args string) string {
	args = strings.TrimSpace(args)
	words := strings.Fields(args)
	body := c.Body
	used := strings.Contains(body, "$ARGUMENTS")
	body = strings.ReplaceAll(body, "$ARGUMENTS", args)
	for i := 9; i >= 1; i-- { // $10 isn't a thing; go down so $1 doesn't eat $1x
		key := "$" + strconv.Itoa(i)
		if strings.Contains(body, key) {
			used = true
			v := ""
			if i <= len(words) {
				v = words[i-1]
			}
			body = strings.ReplaceAll(body, key, v)
		}
	}
	if !used && args != "" {
		body += "\n\n" + args
	}
	return body
}

// Bundled returns the commands built into Blitz.
func Bundled() []Command {
	entries, _ := fs.ReadDir(builtinFS, "builtin")
	var out []Command
	for _, e := range entries {
		data, err := builtinFS.ReadFile("builtin/" + e.Name())
		if err != nil {
			continue
		}
		if c, err := Parse(strings.TrimSuffix(e.Name(), ".md"), "bundled", "", data); err == nil {
			out = append(out, c)
		}
	}
	return out
}

// Load reads the commands under dir: every *.md file, named after its path
// with subdirectories as namespaces (db/migrate.md is db:migrate).
// Problems are returned alongside what loaded.
func Load(dir, source string) ([]Command, error) {
	var out []Command
	var errs []error
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == dir && errors.Is(err, fs.ErrNotExist) {
				return fs.SkipAll
			}
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(strings.ToLower(d.Name()), ".md") {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		name := strings.ToLower(strings.TrimSuffix(filepath.ToSlash(rel), filepath.Ext(rel)))
		name = strings.ReplaceAll(name, "/", ":")
		if !ValidName(name) {
			errs = append(errs, fmt.Errorf("%s: %q isn't a command name (letters, digits, - _ .)", p, name))
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			errs = append(errs, err)
			return nil
		}
		c, err := Parse(name, source, p, data)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", p, err))
			return nil
		}
		out = append(out, c)
		return nil
	})
	if err != nil {
		errs = append(errs, err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, errors.Join(errs...)
}
