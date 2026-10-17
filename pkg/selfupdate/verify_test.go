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

package selfupdate

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/testing/ca"
	"github.com/sigstore/sigstore-go/pkg/verify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The release workflow's identity, as GitHub's certificates name it.
const workflow = "https://github.com/retail-cortex/blitz/.github/workflows/release.yml@refs/tags/v0.6.6"

// A certificate without SCTs (the virtual CA's) fails what releases need.
func TestVerifyEntityNeedsSCTs(t *testing.T) {
	vs, err := ca.NewVirtualSigstore()
	require.NoError(t, err)
	entity, err := vs.Sign(workflow, SignerIssuer, []byte("sums"))
	require.NoError(t, err)
	assert.ErrorContains(t, verifyEntity(vs, entity, strings.NewReader("sums"), verifierOptions...), "signed certificate timestamp")
}

// Only the release workflow's signature of the very checksums, on a
// release tag, from GitHub's issuer, verifies.
func TestVerifyEntity(t *testing.T) {
	vs, err := ca.NewVirtualSigstore()
	require.NoError(t, err)
	sums := []byte("abc  blitz_0.6.6_darwin_arm64.tar.gz\n")
	for _, tc := range []struct {
		name, identity, issuer string
		artifact               []byte
		ok                     bool
	}{
		{"the release workflow", workflow, SignerIssuer, sums, true},
		{"other checksums", workflow, SignerIssuer, []byte("tampered"), false},
		{"another workflow", "https://github.com/retail-cortex/blitz/.github/workflows/ci.yml@refs/heads/main", SignerIssuer, sums, false},
		{"a branch, not a tag", "https://github.com/retail-cortex/blitz/.github/workflows/release.yml@refs/heads/main", SignerIssuer, sums, false},
		{"another repository", "https://github.com/someone/blitz/.github/workflows/release.yml@refs/tags/v0.6.6", SignerIssuer, sums, false},
		{"another issuer", workflow, "https://accounts.google.com", sums, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entity, err := vs.Sign(tc.identity, tc.issuer, sums)
			require.NoError(t, err)
			// The virtual CA's certificates have no SCTs: the rest of what a
			// release's signature must have.
			err = verifyEntity(vs, entity, bytes.NewReader(tc.artifact), verify.WithTransparencyLog(1), verify.WithObserverTimestamps(1))
			if tc.ok {
				assert.NoError(t, err)
			} else {
				assert.ErrorContains(t, err, "doesn't verify")
			}
		})
	}
}

// A bundle in cosign's older format is said to be one; what isn't a
// bundle, or a trust root that can't be had, is an error.
func TestVerifySignature(t *testing.T) {
	ctx := context.Background()
	old, err := os.ReadFile(filepath.Join("testdata", "cosign-v2.sigstore.json"))
	require.NoError(t, err)
	assert.ErrorIs(t, VerifySignature(ctx, strings.NewReader("x"), old, ""), ErrOldBundle)
	assert.ErrorContains(t, VerifySignature(ctx, strings.NewReader("x"), []byte("not json"), ""), "reading the release's signature")

	saved := TrustedRoot
	t.Cleanup(func() { TrustedRoot = saved })
	TrustedRoot = func(context.Context, string) (root.TrustedMaterial, error) { return nil, errors.New("offline") }
	bundle, err := os.ReadFile(filepath.Join("testdata", "bundle-v0.3.sigstore.json"))
	require.NoError(t, err)
	assert.ErrorContains(t, VerifySignature(ctx, strings.NewReader("x"), bundle, t.TempDir()), "getting Sigstore's trust root: offline")
}

// The trust root comes through Sigstore's TUF repository, cached where
// asked (checked without fetching it: tests stay off the network).
func TestTrustOptions(t *testing.T) {
	dir := t.TempDir()
	assert.Equal(t, filepath.Join(dir, "sigstore"), trustOptions(context.Background(), dir).CachePath)
	assert.Contains(t, trustOptions(context.Background(), "").CachePath, ".sigstore")
}

// A real bundle that isn't the release workflow's (sigstore-go's test
// data) is read, then refused.
func TestVerifySignatureRefusesAnotherIdentity(t *testing.T) {
	saved := TrustedRoot
	t.Cleanup(func() { TrustedRoot = saved })
	vs, err := ca.NewVirtualSigstore()
	require.NoError(t, err)
	TrustedRoot = func(context.Context, string) (root.TrustedMaterial, error) { return vs, nil }
	bundle, err := os.ReadFile(filepath.Join("testdata", "bundle-v0.3.sigstore.json"))
	require.NoError(t, err)
	assert.ErrorContains(t, VerifySignature(context.Background(), strings.NewReader("x"), bundle, ""), "doesn't verify")
}
