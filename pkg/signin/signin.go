// Copyright 2026 Retail Cortex
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package signin signs in to model providers with each vendor's own tool
// (gcloud, ant, aws, az), for the Blitz service, so the credentials land
// where the service reads them. It adds to API keys, never replaces them:
// a provider set up with a key keeps it.
package signin

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/shellpath"
)

// The providers Blitz signs in to.
const (
	Google    = "google"    // Gemini and Claude on Vertex AI (auth = "adc")
	Anthropic = "anthropic" // a Claude Console account (auth = "oauth")
	AWS       = "aws"       // Claude on Amazon Bedrock
	Azure     = "azure"     // Azure OpenAI and Claude on Azure (auth = "entra")
)

// Providers are the providers, in the order they're shown.
var Providers = []string{Google, Anthropic, AWS, Azure}

var (
	// ErrUnknownProvider is a provider Blitz doesn't sign in to.
	ErrUnknownProvider = errors.New("unknown provider: use google, anthropic, aws or azure")
	// ErrBadProfile is a profile name that isn't one.
	ErrBadProfile = errors.New("a profile is letters, digits, '.', '_' and '-' (at most 64)")
	// ErrNoTool is a vendor tool that isn't installed.
	ErrNoTool = errors.New("isn't installed")
	// ErrRunning is a sign-in to a provider already under way.
	ErrRunning = errors.New("a sign-in to it is already under way")
)

// timeout bounds one sign-in: the user finishing it in the browser.
var timeout = 10 * time.Minute

// waitDelay bounds waiting for a stopped tool's output to close.
var waitDelay = 2 * time.Second

// statusTimeout bounds checking a provider's credentials.
var statusTimeout = 5 * time.Second

// profileName is what a profile name may be.
var profileName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

// provider is how Blitz signs in to one provider.
type provider struct {
	tool    string // the vendor's program
	install string // where to get it
	login   func(profile string) (args, env []string)
	logout  func(profile string) (args, env []string)
	status  func(ctx context.Context, cfg *config.Config, profile string) (signedIn bool, detail string)
}

var providers = map[string]provider{
	Google: {
		tool: "gcloud", install: "https://cloud.google.com/sdk/docs/install",
		login: func(string) ([]string, []string) { return []string{"auth", "application-default", "login"}, nil },
		logout: func(string) ([]string, []string) {
			return []string{"auth", "application-default", "revoke", "--quiet"}, nil
		},
		status: googleStatus,
	},
	Anthropic: {
		tool: "ant", install: "https://docs.anthropic.com/en/docs/build-with-claude/ant-cli",
		login: func(p string) ([]string, []string) {
			return []string{"auth", "login"}, profileEnv("ANTHROPIC_PROFILE", p)
		},
		logout: func(p string) ([]string, []string) {
			return []string{"auth", "logout"}, profileEnv("ANTHROPIC_PROFILE", p)
		},
		status: anthropicStatus,
	},
	AWS: {
		tool: "aws", install: "https://aws.amazon.com/cli/",
		login:  func(p string) ([]string, []string) { return withProfile([]string{"sso", "login"}, p), nil },
		logout: func(p string) ([]string, []string) { return withProfile([]string{"sso", "logout"}, p), nil },
		status: awsStatus,
	},
	Azure: {
		tool: "az", install: "https://learn.microsoft.com/cli/azure/install-azure-cli",
		login:  func(string) ([]string, []string) { return []string{"login"}, nil },
		logout: func(string) ([]string, []string) { return []string{"logout"}, nil },
		status: azureStatus,
	},
}

func profileEnv(key, p string) []string {
	if p == "" {
		return nil
	}
	return []string{key + "=" + p}
}

func withProfile(args []string, p string) []string {
	if p == "" {
		return args
	}
	return append(args, "--profile", p)
}

// lookup finds a provider and checks a profile name.
func lookup(name, profile string) (provider, error) {
	p, ok := providers[name]
	if !ok {
		return provider{}, fmt.Errorf("%w (%q)", ErrUnknownProvider, name)
	}
	if profile != "" && !profileName.MatchString(profile) {
		return provider{}, fmt.Errorf("%w: %q", ErrBadProfile, profile)
	}
	return p, nil
}

// find finds a vendor tool; tests replace it.
var find = shellpath.Find

// Status is a provider's sign-in: its tool, whether it's installed (and
// where to get it), and whether its credentials work now, with a detail
// (the account, profile or file).
type Status struct {
	Provider, Tool, Install string
	ToolFound, SignedIn     bool
	Detail                  string
}

// StatusOf checks provider's sign-in with cfg's settings (profile, when
// given, over the configured one). The check is local or brief: at most
// statusTimeout.
func StatusOf(ctx context.Context, cfg *config.Config, name, profile string) (Status, error) {
	p, err := lookup(name, profile)
	if err != nil {
		return Status{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	s := Status{Provider: name, Tool: p.tool, Install: p.install, ToolFound: find(p.tool) != ""}
	s.SignedIn, s.Detail = p.status(ctx, cfg, profile)
	return s, nil
}

// Event is a line the vendor's tool printed, with the sign-in page's URL
// and a device code when the line has them.
type Event struct {
	Line, URL, Code string
}

var (
	urlPattern  = regexp.MustCompile(`https://[^\s"'<>]+`)
	codePattern = regexp.MustCompile(`\b[A-Z0-9]{4}-[A-Z0-9]{4}\b`)
)

// parse pulls a URL and a device code ("ABCD-1234") out of a line.
func parse(line string) Event {
	return Event{Line: line, URL: strings.TrimRight(urlPattern.FindString(line), ".,)"), Code: codePattern.FindString(line)}
}

// Runner runs sign-ins and sign-outs, one per provider at a time.
type Runner struct {
	mu      sync.Mutex
	running map[string]bool
}

// claim marks a provider's sign-in under way; release ends it.
func (r *Runner) claim(name string) (release func(), err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running[name] {
		return nil, fmt.Errorf("%s: %w", name, ErrRunning)
	}
	if r.running == nil {
		r.running = map[string]bool{}
	}
	r.running[name] = true
	return func() {
		r.mu.Lock()
		delete(r.running, name)
		r.mu.Unlock()
	}, nil
}

// SignIn runs provider's sign-in (with profile, when given), calling on
// with each line its tool prints, until it ends, at most timeout. The tool
// opens the browser itself; the lines carry the URL (and a device code)
// for when it can't.
func (r *Runner) SignIn(ctx context.Context, name, profile string, on func(Event)) error {
	p, err := lookup(name, profile)
	if err != nil {
		return err
	}
	args, env := p.login(profile)
	return r.run(ctx, name, p, args, env, on)
}

// SignOut runs provider's sign-out (with profile, when given).
func (r *Runner) SignOut(ctx context.Context, name, profile string) error {
	p, err := lookup(name, profile)
	if err != nil {
		return err
	}
	args, env := p.logout(profile)
	return r.run(ctx, name, p, args, env, func(Event) {})
}

// keptLines is how many of the last lines a failure reports.
const keptLines = 5

func (r *Runner) run(ctx context.Context, name string, p provider, args, env []string, on func(Event)) error {
	bin := find(p.tool)
	if bin == "" {
		return fmt.Errorf("%s %w: get it from %s", p.tool, ErrNoTool, p.install)
	}
	release, err := r.claim(name)
	if err != nil {
		return err
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdin = nil           // nothing to answer: the browser does the asking
	cmd.WaitDelay = waitDelay // a child of the tool holding its output can't hold this up
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		return err
	}
	var last []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		sc := bufio.NewScanner(pr)
		for sc.Scan() {
			line := strings.TrimRight(sc.Text(), "\r")
			if strings.TrimSpace(line) == "" {
				continue
			}
			if last = append(last, line); len(last) > keptLines {
				last = last[1:]
			}
			on(parse(line))
		}
		io.Copy(io.Discard, pr) // a line too long for the scanner
	}()
	err = cmd.Wait()
	pw.Close()
	<-done
	switch {
	case ctx.Err() != nil:
		return fmt.Errorf("%s %s: %w", p.tool, strings.Join(args, " "), context.Cause(ctx))
	case err != nil:
		return fmt.Errorf("%s %s: %w: %s", p.tool, strings.Join(args, " "), err, strings.Join(last, " / "))
	}
	return nil
}
