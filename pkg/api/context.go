package api

import (
	"errors"
)

// ContextInfo is the size of the model's context and when it is compacted.
type ContextInfo struct {
	Tokens int64 // the latest prompt's size
	// AutoCompact: the context is compacted when it passes Threshold
	// tokens, keeping the last Keep events.
	AutoCompact bool
	Threshold   int
	Keep        int
}

// CompactResult is what Compact did.
type CompactResult struct {
	EventsCompacted int
	SummaryChars    int
	Before, After   Usage
}

// LocaleInfo names an interface language with a catalog.
type LocaleInfo struct{ Tag, Name string }

// ErrUnknownLocale reports input that isn't a language.
var ErrUnknownLocale = errors.New("not a language")

// LocaleChange describes the interface language after SetLocale.
type LocaleChange struct {
	Tag          string
	NativeName   string // e.g. "Español"
	LanguageName string // in the new language's terms, e.g. "español"
	// HasCatalog: the interface is translated; otherwise only the model's
	// replies are in the language.
	HasCatalog bool
	Saved      Saved
}

// ErrNoFetch reports that the agent can't fetch web pages, so a web search
// would give it nothing to read.
var ErrNoFetch = errors.New("web fetching is disabled")

// Link is a web search result the agent is asked to read.
type Link struct{ Title, URL string }

// WebSearch is a search whose results are handed to the agent: run Prompt
// as a turn with FetchGrants set to the links' URLs, so the agent may read
// exactly those pages without asking.
type WebSearch struct {
	Links  []Link // none: nothing worth handing over
	Prompt string
}

// URLs returns the links' URLs, for Turn.FetchGrants.
func (s WebSearch) URLs() []string {
	out := make([]string, len(s.Links))
	for i, l := range s.Links {
		out[i] = l.URL
	}
	return out
}

// ErrNothingToCompact is returned when a session is too short to compact.
var ErrNothingToCompact = errors.New("nothing to compact yet")

// ErrNoSearch is returned by Registry.WebSearch when no search provider is
// configured (web.search_provider), or it can't be used.
var ErrNoSearch = errors.New("web search is not set up")
