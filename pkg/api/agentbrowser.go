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

package api

// AgentBrowserCommand is what a desktop window is asked to do with a view
// of the agent's browser (spec_agent_browser_050): open one, pass it a
// DevTools message, or close it.
type AgentBrowserCommand struct {
	// BrowserID names the view, the same in every command and reply.
	BrowserID string
	// Kind is "open", "message" or "close"; or, for a page the user asked
	// to see (AB-10), "page" (a web tab) or "external" (the system's
	// browser), with URL and no BrowserID.
	Kind string
	// Message is a DevTools Protocol command as JSON (Kind "message").
	Message string
	// Show is "watch" (a tab the user sees) or "silent" (off screen), and
	// Profile the browser profile whose sign-ins it uses ("": none, a
	// throwaway session), for "open".
	Show    string
	Profile string
	// AllowLocal lets it reach loopback and private addresses, and
	// DenyDomains are never reached (web.deny_domains), for "open".
	AllowLocal  bool
	DenyDomains []string
	// URL is the page to show, for "page" and "external".
	URL string
}
