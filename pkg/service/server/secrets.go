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

package server

import (
	"context"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/secrets"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// The user's secrets (spec_secrets_039), for Settings › Secrets and a
// workspace's settings: names listed, values only ever written.

// secretScope is the vault and scope a request's workspace names.
func (h configService) secretScope(workspace string) (string, error) {
	dir, err := scope(workspace)
	if err != nil {
		return "", err
	}
	return config.SecretScope(h.s.configDir, dir), nil
}

func (h configService) ListSecrets(_ context.Context, r req[pb.ListSecretsRequest]) (*connect.Response[pb.ListSecretsResponse], error) {
	sc, err := h.secretScope(r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	vault := config.Vault(h.s.configDir)
	list, err := vault.List(sc)
	if err != nil {
		return nil, err
	}
	out := &pb.ListSecretsResponse{Store: vault.Store.Kind(), Prefix: secrets.URN(sc, "")}
	for _, e := range list {
		out.Secrets = append(out.Secrets, &pb.Secret{Key: e.Key, Updated: timestamppb.New(e.Updated)})
	}
	return ok(out)
}

func (h configService) SetSecret(_ context.Context, r req[pb.SetSecretRequest]) (*connect.Response[pb.SetSecretResponse], error) {
	sc, err := h.secretScope(r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if err := config.Vault(h.s.configDir).Set(sc, r.Msg.Key, r.Msg.Value); err != nil {
		return nil, invalid(err)
	}
	return ok(&pb.SetSecretResponse{})
}

func (h configService) DeleteSecret(_ context.Context, r req[pb.DeleteSecretRequest]) (*connect.Response[pb.DeleteSecretResponse], error) {
	sc, err := h.secretScope(r.Msg.Workspace)
	if err != nil {
		return nil, err
	}
	if err := config.Vault(h.s.configDir).Delete(sc, r.Msg.Key); err != nil {
		return nil, err
	}
	return ok(&pb.DeleteSecretResponse{})
}
