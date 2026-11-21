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

package enterprise

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/retail-cortex/blitz/pkg/netutil"
)

// SharedMCP is a shared connector's kind when it's a remote MCP server.
const SharedMCP = "mcp"

// SharedConnector is one of the organization's shared connectors the
// person's policy lets them use (spec_053 EN-70; the service's
// spec_013_connectors CN-20): the service holds the connection and its
// secrets, and runs their queries.
type SharedConnector struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
	// ReadOnly: their queries can't change data (the connector's, or
	// their policy's doing).
	ReadOnly bool `json:"read_only"`
	// Tables are the patterns of the tables they may use; every one when
	// none.
	Tables  []string `json:"tables,omitempty"`
	MaxRows int      `json:"max_rows"`
	// Auth is person when it queries as them, with their Google consent
	// (EN-76; the service's CN-60).
	Auth string `json:"auth,omitempty"`
}

// SharedAsPerson is a shared connector's auth when it queries as each
// person.
const SharedAsPerson = "person"

// ErrNoConsent is a service that can't query as each person (another
// sign-in provider, or one before it).
var ErrNoConsent = errors.New("your organization's service can't query as each person")

// ConnectorConsent is where the person lets connectors query as them,
// with their Google account, and whether they have (EN-76).
func (c *Client) ConnectorConsent(ctx context.Context) (string, bool, error) {
	var out struct {
		URL       string `json:"url"`
		Consented bool   `json:"consented"`
	}
	err := c.signedCall(ctx, http.MethodGet, "/api/v1/connectors/consent", nil, &out)
	if ref, ok := errors.AsType[*Refusal](err); ok && ref.Status == http.StatusNotFound {
		return "", false, ErrNoConsent
	}
	return out.URL, out.Consented, err
}

// RevokeConnectorConsent forgets the person's consent: those connectors
// ask again.
func (c *Client) RevokeConnectorConsent(ctx context.Context) error {
	return c.signedCall(ctx, http.MethodDelete, "/api/v1/connectors/consent", nil, nil)
}

// SharedTable is one of a shared connector's tables they may use.
type SharedTable struct {
	Name    string `json:"name"`
	About   string `json:"about,omitempty"`
	Columns []struct {
		Name  string `json:"name"`
		Type  string `json:"type,omitempty"`
		About string `json:"about,omitempty"`
	} `json:"columns"`
}

// SharedConnectors are the shared connectors the person may use; none
// from a service before them.
func (c *Client) SharedConnectors(ctx context.Context) ([]SharedConnector, error) {
	var out struct {
		Connectors []SharedConnector `json:"connectors"`
	}
	err := c.signedCall(ctx, http.MethodGet, "/api/v1/connectors", nil, &out)
	if ref, ok := errors.AsType[*Refusal](err); ok && ref.Status == http.StatusNotFound {
		return nil, nil
	}
	return out.Connectors, err
}

// SharedSchema are the tables of the shared connector named name the
// person may use.
func (c *Client) SharedSchema(ctx context.Context, name string) ([]SharedTable, error) {
	var out struct {
		Tables []SharedTable `json:"tables"`
	}
	err := c.signedCall(ctx, http.MethodGet, "/api/v1/databases/"+url.PathEscape(name)+"/schema", nil, &out)
	return out.Tables, err
}

// SharedQuery runs statement through the shared connector named name, as
// the person, at most maxRows rows (0: as many as their policy allows);
// the result is the service's table, as Blitz's data tables are in JSON.
// A refusal says why and who decided it (a *Refusal, 403).
func (c *Client) SharedQuery(ctx context.Context, name, statement string, maxRows int) (json.RawMessage, error) {
	var out json.RawMessage
	err := c.signedCall(ctx, http.MethodPost, "/api/v1/databases/"+url.PathEscape(name)+"/query", map[string]any{"statement": statement, "max_rows": maxRows}, &out)
	return out, err
}

// MCPEndpoint is the service's endpoint for the shared connector name
// when it's a remote MCP server: MCP's streamable HTTP, through the
// service, which adds the organization's token (the service's CN-51).
func (c *Client) MCPEndpoint(name string) string {
	return c.url + "/api/v1/mcp/" + url.PathEscape(name)
}

// SignedHTTP is an HTTP client whose requests carry the person's access
// token, renewed as it needs to be.
func (c *Client) SignedHTTP() *http.Client {
	return &http.Client{Transport: signedTransport{c: c, base: c.hc.Transport}}
}

type signedTransport struct {
	c    *Client
	base http.RoundTripper
}

func (t signedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token, err := t.c.AccessToken(req.Context())
	if err != nil {
		return nil, err
	}
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+token)
	base := t.base
	if base == nil {
		base = netutil.CloneDefaultTransport() // with [network] ca_file
	}
	return base.RoundTrip(req)
}
