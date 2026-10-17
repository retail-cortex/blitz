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

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"

	"github.com/retail-cortex/blitz/pkg/config"
	"github.com/retail-cortex/blitz/pkg/selfupdate"
	"github.com/spf13/cobra"
)

// What `blitz update` uses; variables for tests.
var (
	releases        = selfupdate.GitHub
	executable      = os.Executable
	verifySignature = selfupdate.VerifySignature
	lookCosign      = func() (string, error) { return exec.LookPath("cosign") }
)

// newUpdateCommand is `blitz update`: install the latest release over this
// one, after checking its signature and checksum (spec_parity_027
// PAR-MOD-06).
func newUpdateCommand() *cobra.Command {
	var check, skipSignature bool
	var want string
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update Blitz to the latest release (its signature and checksum checked)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Minute)
			defer cancel()
			tag := want
			if tag == "" {
				latest, err := releases().Latest(ctx)
				if err != nil {
					return fmt.Errorf("finding the latest release: %w", err)
				}
				tag = latest
			}
			if !strings.HasPrefix(tag, "v") {
				tag = "v" + tag
			}
			newer := version == "dev" || selfupdate.Compare(tag, version) > 0
			switch {
			case check && newer:
				fmt.Fprintf(out, "Blitz %s is available (this is %s): blitz update\n", tag, version)
				return nil
			case check:
				fmt.Fprintf(out, "Blitz %s is the latest release.\n", version)
				return nil
			case version == "dev" && want == "":
				return withCode(exitUsage, errors.New("this is a development build: name a release with --version, or install one"))
			case !newer && want == "":
				fmt.Fprintf(out, "Blitz %s is the latest release.\n", version)
				return nil
			}
			exe, err := executable()
			if err != nil {
				return err
			}
			if real, err := filepath.EvalSymlinks(exe); err == nil {
				exe = real
			}
			if manager := selfupdate.PackageManager(exe); manager != "" {
				return withCode(exitUsage, fmt.Errorf("installed by %s: update it there", manager))
			}
			if err := installRelease(ctx, out, tag, exe, skipSignature); err != nil {
				return err
			}
			fmt.Fprintf(out, "Updated to Blitz %s. If the Blitz service is running: blitz service restart\n", tag)
			return nil
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "Only say whether a newer release exists")
	cmd.Flags().StringVar(&want, "version", "", "Install this release (vX.Y.Z) instead of the latest")
	cmd.Flags().BoolVar(&skipSignature, "skip-signature", false, "Install without checking the release's signature (the checksum is still checked)")
	return cmd
}

// installRelease downloads tag's archive for this machine, checks it, and
// puts its blitz and blitzd over exe and the blitzd beside it.
func installRelease(ctx context.Context, out io.Writer, tag, exe string, skipSignature bool) error {
	ver := strings.TrimPrefix(tag, "v")
	ext := ".tar.gz"
	if goruntime.GOOS == "windows" {
		ext = ".zip"
	}
	name := fmt.Sprintf("blitz_%s_%s_%s%s", ver, goruntime.GOOS, goruntime.GOARCH, ext)
	tmp, err := os.MkdirTemp("", "blitz-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	fmt.Fprintf(out, "Downloading Blitz %s for %s/%s…\n", tag, goruntime.GOOS, goruntime.GOARCH)
	src := releases()
	for _, f := range []string{name, "checksums.txt", "checksums.txt.sigstore.json"} {
		if err := selfupdate.Fetch(ctx, src.File(tag, f), filepath.Join(tmp, f)); err != nil {
			return fmt.Errorf("downloading %s: %w", f, err)
		}
	}
	sums := filepath.Join(tmp, "checksums.txt")
	if skipSignature {
		fmt.Fprintln(out, "Not checking the signature (--skip-signature); the checksum is still checked.")
	} else if err := checkSignature(ctx, sums, filepath.Join(tmp, "checksums.txt.sigstore.json")); err != nil {
		return err
	} else {
		fmt.Fprintln(out, "The checksums are signed by Blitz's release workflow.")
	}
	if err := selfupdate.CheckSum(sums, filepath.Join(tmp, name)); err != nil {
		return err
	}
	files, err := selfupdate.ExtractPrograms(filepath.Join(tmp, name), tmp, "blitz", "blitzd")
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	targets := map[string]string{"blitz": exe}
	if daemon := filepath.Join(dir, selfupdate.ProgramName("blitzd")); selfupdate.FileExists(daemon) {
		targets["blitzd"] = daemon
	}
	// Both programs or neither: a blitz newer than its blitzd won't do.
	var pairs [][2]string
	for _, prog := range []string{"blitzd", "blitz"} {
		dst, ok := targets[prog]
		if !ok {
			continue
		}
		if _, ok := files[prog]; !ok {
			return fmt.Errorf("the archive has no %s", prog)
		}
		pairs = append(pairs, [2]string{files[prog], dst})
	}
	return selfupdate.ReplaceFiles(pairs)
}

// checkSignature checks the checksums' signature, here (Sigstore's bundle,
// releases from v0.6.6 on), else with cosign for an older release's.
func checkSignature(ctx context.Context, sums, bundle string) error {
	data, err := os.ReadFile(sums)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(bundle)
	if err != nil {
		return err
	}
	err = verifySignature(ctx, bytes.NewReader(data), b, config.ExpandHome("~/.blitz"))
	if !errors.Is(err, selfupdate.ErrOldBundle) {
		if err != nil {
			return fmt.Errorf("%w: nothing was installed", err)
		}
		return nil
	}
	cosign, lookErr := lookCosign()
	if lookErr != nil {
		return withCode(exitUsage, errors.New("this release's signature can only be checked with cosign, which isn't installed: install cosign (https://docs.sigstore.dev), or use --skip-signature"))
	}
	c := exec.CommandContext(ctx, cosign, "verify-blob", "--bundle", bundle,
		"--certificate-identity-regexp", selfupdate.SignerIdentity, "--certificate-oidc-issuer", selfupdate.SignerIssuer, sums)
	if msg, err := c.CombinedOutput(); err != nil {
		return fmt.Errorf("the release's signature doesn't verify, so nothing was installed: %s", strings.TrimSpace(string(msg)))
	}
	return nil
}
