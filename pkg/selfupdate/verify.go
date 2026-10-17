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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
	"github.com/sigstore/sigstore-go/pkg/verify"
)

// The release workflow signs keylessly: a certificate for it, from
// GitHub's OIDC issuer, on a release tag.
const (
	SignerIdentity = `^https://github.com/retail-cortex/blitz/\.github/workflows/release\.yml@refs/tags/v`
	SignerIssuer   = "https://token.actions.githubusercontent.com"
)

// ErrOldBundle is a signature in cosign's older bundle (releases before
// v0.6.6), which only cosign itself checks.
var ErrOldBundle = errors.New("the release's signature is in cosign's older format: check it with cosign")

// TrustedRoot is Sigstore's public trust root (its certificate
// authorities, transparency logs and timestamping), kept current through
// its TUF repository and cached in cacheDir; tests replace it.
var TrustedRoot = func(ctx context.Context, cacheDir string) (root.TrustedMaterial, error) {
	return root.FetchTrustedRootWithOptions(trustOptions(ctx, cacheDir))
}

// trustOptions are how the trust root is fetched: Sigstore's public TUF
// repository, cached in cacheDir/sigstore (the user's ~/.sigstore when "").
func trustOptions(ctx context.Context, cacheDir string) *tuf.Options {
	opts := tuf.DefaultOptions().WithContext(ctx)
	if cacheDir != "" {
		opts = opts.WithCachePath(filepath.Join(cacheDir, "sigstore"))
	}
	return opts
}

// VerifySignature checks that bundleJSON (a Sigstore bundle, as the
// release workflow writes beside checksums.txt and each desktop package)
// is the release workflow's signature of artifact, read once as it's
// checked: its certificate is for the workflow on a release
// tag, from GitHub's issuer, logged in Sigstore's transparency log.
// cacheDir keeps Sigstore's trust root between checks ("" for the user's
// default).
func VerifySignature(ctx context.Context, artifact io.Reader, bundleJSON []byte, cacheDir string) error {
	if isOldBundle(bundleJSON) {
		return ErrOldBundle
	}
	var b bundle.Bundle
	if err := b.UnmarshalJSON(bundleJSON); err != nil {
		return fmt.Errorf("reading the release's signature: %w", err)
	}
	trusted, err := TrustedRoot(ctx, cacheDir)
	if err != nil {
		return fmt.Errorf("getting Sigstore's trust root: %w", err)
	}
	return verifyEntity(trusted, &b, artifact, verifierOptions...)
}

// verifierOptions are what a release's signature must have: its entry in
// Sigstore's transparency log, a time it was observed at, and the
// certificate's own transparency (SCTs, which Fulcio's certificates carry).
var verifierOptions = []verify.VerifierOption{verify.WithTransparencyLog(1), verify.WithObserverTimestamps(1), verify.WithSignedCertificateTimestamps(1)}

// verifyEntity checks entity's signature of artifact against trusted, for
// the release workflow's identity.
func verifyEntity(trusted root.TrustedMaterial, entity verify.SignedEntity, artifact io.Reader, opts ...verify.VerifierOption) error {
	v, err := verify.NewVerifier(trusted, opts...)
	if err != nil {
		return err
	}
	id, err := verify.NewShortCertificateIdentity(SignerIssuer, "", "", SignerIdentity)
	if err != nil {
		return err
	}
	if _, err := v.Verify(entity, verify.NewPolicy(verify.WithArtifact(artifact), verify.WithCertificateIdentity(id))); err != nil {
		return fmt.Errorf("the release's signature doesn't verify: %w", err)
	}
	return nil
}

// isOldBundle reports whether data is cosign's older bundle (a signature,
// a certificate and a Rekor entry at its top level).
func isOldBundle(data []byte) bool {
	var b struct {
		Signature string          `json:"base64Signature"`
		Rekor     json.RawMessage `json:"rekorBundle"`
	}
	return json.Unmarshal(data, &b) == nil && b.Signature != "" && len(b.Rekor) > 0
}
