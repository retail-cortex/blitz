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
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"
	"github.com/retail-cortex/blitz/pkg/secrets"
	"github.com/retail-cortex/blitz/pkg/skillreg"
	pb "github.com/retail-cortex/blitz/proto/blitz/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Settings › Skills over the API: add a registry, search it, install,
// update and uninstall, and remove it.
func TestSkillRegistriesOverTheAPI(t *testing.T) {
	keys := &secrets.Memory{}
	secrets.SetDefault(keys)
	_, s := serve(t, nil)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	cfg := pb.NewConfigServiceClient(http.DefaultClient, srv.URL)
	ctx := context.Background()

	reg := t.TempDir()
	var zbuf bytes.Buffer
	z := zip.NewWriter(&zbuf)
	f, _ := z.Create("SKILL.md")
	_, _ = f.Write([]byte("---\nname: lit-review\ndescription: Reviews literature\n---\nRead.\n"))
	require.NoError(t, z.Close())
	sum := sha256.Sum256(zbuf.Bytes())
	require.NoError(t, os.WriteFile(filepath.Join(reg, "lit.zip"), zbuf.Bytes(), 0o644))
	idx, _ := json.Marshal(skillreg.Index{Version: 1, Name: "Castor", Skills: []skillreg.Entry{{Name: "lit-review", Description: "Reviews literature", Version: "1.0.0", ArchiveURL: "lit.zip", SHA256: hex.EncodeToString(sum[:]), CastorID: "sk_1"}}})
	index := filepath.Join(reg, "index.json")
	require.NoError(t, os.WriteFile(index, idx, 0o644))

	_, err := cfg.AddSkillRegistry(ctx, connect.NewRequest(&pb.AddSkillRegistryRequest{}))
	assert.Equal(t, connect.CodeInvalidArgument, connect.CodeOf(err))
	_, err = cfg.AddSkillRegistry(ctx, connect.NewRequest(&pb.AddSkillRegistryRequest{Url: filepath.Join(reg, "missing.json")}))
	assert.Equal(t, "REGISTRY_FAILED", reasonOf(err))
	added, err := cfg.AddSkillRegistry(ctx, connect.NewRequest(&pb.AddSkillRegistryRequest{Url: index, Token: "t0k"}))
	require.NoError(t, err)
	assert.EqualValues(t, 1, added.Msg.Skills)
	assert.Equal(t, "castor", added.Msg.Registry.Name)
	assert.True(t, added.Msg.Registry.HasToken)

	list, err := cfg.ListSkillRegistries(ctx, connect.NewRequest(&pb.ListSkillRegistriesRequest{}))
	require.NoError(t, err)
	require.Len(t, list.Msg.Registries, 1)
	assert.True(t, list.Msg.Registries[0].HasToken)

	found, err := cfg.SearchRegistrySkills(ctx, connect.NewRequest(&pb.SearchRegistrySkillsRequest{Query: "literature"}))
	require.NoError(t, err)
	require.Len(t, found.Msg.Skills, 1)
	assert.Equal(t, "sk_1", found.Msg.Skills[0].CastorId)
	assert.Empty(t, found.Msg.Problems)

	_, err = cfg.InstallRegistrySkill(ctx, connect.NewRequest(&pb.InstallRegistrySkillRequest{Ref: "ghost"}))
	assert.Equal(t, "NOT_OFFERED", reasonOf(err))
	_, err = cfg.InstallRegistrySkill(ctx, connect.NewRequest(&pb.InstallRegistrySkillRequest{Ref: "lit-review", Workspace: "relative"}))
	assert.Equal(t, "INVALID_WORKSPACE", reasonOf(err))
	ws := t.TempDir()
	in, err := cfg.InstallRegistrySkill(ctx, connect.NewRequest(&pb.InstallRegistrySkillRequest{Ref: "castor/lit-review", Workspace: ws}))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(ws, ".agents", "skills", "lit-review"), in.Msg.Skill.Dir)
	_, err = cfg.InstallRegistrySkill(ctx, connect.NewRequest(&pb.InstallRegistrySkillRequest{Ref: "lit-review"}))
	require.NoError(t, err, "and for the user")

	up, err := cfg.UpdateRegistrySkills(ctx, connect.NewRequest(&pb.UpdateRegistrySkillsRequest{}))
	require.NoError(t, err)
	assert.Empty(t, up.Msg.Updated, "up to date")

	_, err = cfg.UninstallRegistrySkill(ctx, connect.NewRequest(&pb.UninstallRegistrySkillRequest{Name: "lit-review"}))
	require.NoError(t, err)
	_, err = cfg.UninstallRegistrySkill(ctx, connect.NewRequest(&pb.UninstallRegistrySkillRequest{Name: "lit-review", Workspace: ws}))
	require.NoError(t, err)
	_, err = cfg.UninstallRegistrySkill(ctx, connect.NewRequest(&pb.UninstallRegistrySkillRequest{Name: "lit-review", Workspace: ws}))
	assert.Equal(t, "NOT_INSTALLED", reasonOf(err))
	_, err = cfg.UninstallRegistrySkill(ctx, connect.NewRequest(&pb.UninstallRegistrySkillRequest{Name: "x", Workspace: "rel"}))
	assert.Equal(t, "INVALID_WORKSPACE", reasonOf(err))

	_, err = cfg.RemoveSkillRegistry(ctx, connect.NewRequest(&pb.RemoveSkillRegistryRequest{Name: "castor"}))
	require.NoError(t, err)
	_, err = cfg.RemoveSkillRegistry(ctx, connect.NewRequest(&pb.RemoveSkillRegistryRequest{Name: "castor"}))
	assert.Equal(t, "NO_REGISTRY", reasonOf(err))
	_, err = keys.Get("skill-registry:castor")
	assert.Error(t, err, "the token went with it")
}

func TestSkillError(t *testing.T) {
	for err, want := range map[error]string{
		skillreg.ErrAmbiguous: "AMBIGUOUS",
		skillreg.ErrHash:      "HASH_MISMATCH",
		skillreg.ErrExists:    "SKILL_EXISTS",
		errors.New("boom"):    "REGISTRY_FAILED",
	} {
		assert.Equal(t, want, reasonOf(skillError(err)))
	}
	assert.Equal(t, []string{"a", "b"}, problems(errors.Join(errors.New("a"), errors.New("b"))))
	assert.Nil(t, problems(nil))
}

func TestSkillStoreFollowsTheSettingsDir(t *testing.T) {
	dir := t.TempDir()
	st := (&Server{configDir: dir}).skillStore()
	assert.Equal(t, filepath.Join(dir, "skill-registries.toml"), st.File)
	assert.Equal(t, filepath.Join(dir, "skills"), st.UserSkills)

	// A broken file: listing says so.
	require.NoError(t, os.WriteFile(st.File, []byte("[[registries"), 0o600))
	h := configService{s: &Server{configDir: dir}}
	_, err := h.ListSkillRegistries(context.Background(), connect.NewRequest(&pb.ListSkillRegistriesRequest{}))
	assert.Equal(t, "REGISTRY_FAILED", reasonOf(err))
	_, err = h.InstallRegistrySkill(context.Background(), connect.NewRequest(&pb.InstallRegistrySkillRequest{Ref: "x"}))
	assert.Equal(t, "REGISTRY_FAILED", reasonOf(err))
	up, err := h.UpdateRegistrySkills(context.Background(), connect.NewRequest(&pb.UpdateRegistrySkillsRequest{}))
	require.NoError(t, err)
	assert.NotEmpty(t, up.Msg.Problems)
}

// The skills Blitz ships with are listed without a workspace, by name.
func TestListBuiltinSkills(t *testing.T) {
	_, s := serve(t, nil)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	res, err := pb.NewConfigServiceClient(http.DefaultClient, srv.URL).ListBuiltinSkills(context.Background(), connect.NewRequest(&pb.ListBuiltinSkillsRequest{}))
	require.NoError(t, err)
	var names []string
	for _, sk := range res.Msg.Skills {
		names = append(names, sk.Name)
		assert.NotEmpty(t, sk.Description, sk.Name)
	}
	assert.Contains(t, names, "code-review")
	assert.IsIncreasing(t, names)
}
