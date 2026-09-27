package api

import (
	"errors"
	"fmt"

	"github.com/retail-cortex/blitz/pkg/config"
)

// UnknownAgentError reports an agent name that isn't registered.
type UnknownAgentError struct{ Name string }

func (e *UnknownAgentError) Error() string { return fmt.Sprintf("unknown agent %q", e.Name) }

// Saved reports where a change was written in the config file. The change
// applies either way; Err is why it wasn't saved.
type Saved struct {
	Path string
	Err  error
}

// AgentInfo describes an agent.
type AgentInfo struct {
	Name        string
	DisplayName string
	Description string
	Active      bool
	// PinnedModel is the model the agent is pinned to ("" when it runs on
	// the configured model).
	PinnedModel string
}

// ModelInfo names the model the active agent runs on.
type ModelInfo struct {
	Name     string
	Provider string // the configured provider
}

// PinResult describes an agent's model after PinModel or Unpin.
type PinResult struct {
	Agent string
	Model string // the model the agent now runs on
	Saved Saved
}

// ErrBadModelRef reports a model reference with no model name.
var ErrBadModelRef = errors.New("not a model name")

// ModelSettingsInfo is one model's generation settings and what applies
// where it has none.
type ModelSettingsInfo struct {
	Model    string // the name settings are kept under, without a provider
	Provider string
	Settings config.ModelSettings
	// GlobalTemperature and GlobalMaxTokens apply when Settings leaves them
	// unset (0: the provider's default).
	GlobalTemperature float64
	GlobalMaxTokens   int
}

// Setting is one key=value change; an empty Value clears the key.
type Setting struct{ Key, Value string }

// ModelSettingsChange describes the result of UpdateModelSettings.
type ModelSettingsChange struct {
	ModelSettingsInfo
	// Unsupported are keys set that the provider or model ignores.
	Unsupported []string
	Saved       Saved
}

// InvalidSettingError reports a setting that can't be applied: an unknown
// key or a value out of range.
type InvalidSettingError struct{ Err error }

func (e *InvalidSettingError) Error() string { return e.Err.Error() }

func (e *InvalidSettingError) Unwrap() error { return e.Err }

// Settings are the values /set changes, plus the model, agent and locale.
type Settings struct {
	Agency string
	Model  ModelInfo
	Agent  string
	Locale string // the language the model replies in
	// PermissionMode decides which actions run without asking (a
	// PermissionMode).
	PermissionMode string
	// Effort is the session's reasoning effort ("" when each model uses its
	// own reasoning_effort or its default).
	Effort string
}

// UnknownSettingError reports a key /set doesn't know.
type UnknownSettingError struct{ Key string }

func (e *UnknownSettingError) Error() string { return fmt.Sprintf("unknown setting %q", e.Key) }

// ErrInvalidAgency reports an agency level other than low, medium, high or extreme.
var ErrInvalidAgency = errors.New("agency must be low, medium, high or extreme")
