// Copyright 2026 Retail Cortex
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/secrets"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Secrets are added, listed by name (never value), replaced and deleted,
// globally and for a workspace.
func TestSecretsOverTheAPI(t *testing.T) {
	store := &secrets.Memory{}
	secrets.SetDefault(store)
	_, s := serve(t, nil)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	cfg := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()
	dir := t.TempDir()
	names := func(ws string) []string {
		res, err := cfg.ListSecrets(ctx, connect.NewRequest(&pb.ListSecretsRequest{Workspace: ws}))
		require.NoError(t, err)
		assert.Equal(t, "memory", res.Msg.Store)
		if ws == "" {
			assert.Equal(t, "urn:blitz:global:secrets:", res.Msg.Prefix)
		} else {
			assert.Regexp(t, `^urn:blitz:.+-[0-9a-f]{8}:secrets:$`, res.Msg.Prefix)
		}
		var out []string
		for _, x := range res.Msg.Secrets {
			out = append(out, x.Key)
			assert.NotNil(t, x.Updated)
		}
		return out
	}

	_, err := cfg.SetSecret(ctx, connect.NewRequest(&pb.SetSecretRequest{Key: "token", Value: "global-token-1"}))
	require.NoError(t, err)
	_, err = cfg.SetSecret(ctx, connect.NewRequest(&pb.SetSecretRequest{Workspace: dir, Key: "db", Value: "workspace-db-1"}))
	require.NoError(t, err)
	assert.Equal(t, []string{"token"}, names(""))
	assert.Equal(t, []string{"db"}, names(dir))
	v, err := store.Get(secrets.URN("global", "token"))
	require.NoError(t, err)
	assert.Equal(t, "global-token-1", v)

	_, err = cfg.SetSecret(ctx, connect.NewRequest(&pb.SetSecretRequest{Key: "bad key", Value: "x"}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	_, err = cfg.ListSecrets(ctx, connect.NewRequest(&pb.ListSecretsRequest{Workspace: "relative"}))
	assert.Error(t, err)

	_, err = cfg.DeleteSecret(ctx, connect.NewRequest(&pb.DeleteSecretRequest{Workspace: dir, Key: "db"}))
	require.NoError(t, err)
	assert.Empty(t, names(dir))
}
