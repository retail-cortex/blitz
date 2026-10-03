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
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// Where releases are; variables for tests.
var (
	releasesAPI      = "https://api.github.com/repos/retail-cortex/blitz"
	releasesDownload = "https://github.com/retail-cortex/blitz/releases/download"
	executable       = os.Executable
	lookCosign       = func() (string, error) { return exec.LookPath("cosign") }
)

// The release workflow signs the checksums keylessly.
const (
	signerIdentity = `^https://github.com/retail-cortex/blitz/\.github/workflows/release\.yml@refs/tags/v`
	signerIssuer   = "https://token.actions.githubusercontent.com"
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
				latest, err := latestRelease(ctx)
				if err != nil {
					return fmt.Errorf("finding the latest release: %w", err)
				}
				tag = latest
			}
			if !strings.HasPrefix(tag, "v") {
				tag = "v" + tag
			}
			newer := version == "dev" || compareVersions(tag, version) > 0
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
			if manager := packageManager(exe); manager != "" {
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
	cmd.Flags().StringVar(&want, "version", "", "Install this release (v0.2.0) instead of the latest")
	cmd.Flags().BoolVar(&skipSignature, "skip-signature", false, "Install without cosign checking the release's signature (the checksum is still checked)")
	return cmd
}

// latestRelease is the latest release's tag.
func latestRelease(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesAPI+"/releases/latest", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var r struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil || r.Tag == "" {
		return "", errors.New("no release tag in the answer")
	}
	return r.Tag, nil
}

// packageManager names what installed exe, if a package manager did.
func packageManager(exe string) string {
	switch {
	case strings.Contains(exe, "/Cellar/") || strings.Contains(exe, "/homebrew/"):
		return "Homebrew (brew upgrade blitz)"
	case strings.HasPrefix(exe, "/usr/bin/") || strings.HasPrefix(exe, "/usr/lib/"):
		return "your system's package manager"
	}
	return ""
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
	for _, f := range []string{name, "checksums.txt", "checksums.txt.sigstore.json"} {
		if err := fetchFile(ctx, releasesDownload+"/"+tag+"/"+f, filepath.Join(tmp, f)); err != nil {
			return fmt.Errorf("downloading %s: %w", f, err)
		}
	}
	sums := filepath.Join(tmp, "checksums.txt")
	switch cosign, err := lookCosign(); {
	case err == nil:
		c := exec.CommandContext(ctx, cosign, "verify-blob", "--bundle", filepath.Join(tmp, "checksums.txt.sigstore.json"),
			"--certificate-identity-regexp", signerIdentity, "--certificate-oidc-issuer", signerIssuer, sums)
		if msg, err := c.CombinedOutput(); err != nil {
			return fmt.Errorf("the release's signature doesn't verify, so nothing was installed: %s", strings.TrimSpace(string(msg)))
		}
		fmt.Fprintln(out, "The checksums are signed by Blitz's release workflow.")
	case skipSignature:
		fmt.Fprintln(out, "Not checking the signature (--skip-signature); the checksum is still checked.")
	default:
		return withCode(exitUsage, errors.New("cosign isn't installed, so the release's signature can't be checked: install cosign (https://docs.sigstore.dev), or use --skip-signature"))
	}
	if err := checkSum(sums, filepath.Join(tmp, name)); err != nil {
		return err
	}
	files, err := extractPrograms(filepath.Join(tmp, name), tmp)
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	targets := map[string]string{"blitz": exe}
	if daemon := filepath.Join(dir, programName("blitzd")); fileExists(daemon) {
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
	return replaceFiles(pairs)
}

func fetchFile(ctx context.Context, url, to string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(to)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, io.LimitReader(resp.Body, 512<<20))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// checkSum checks file against its line in sums.
func checkSum(sums, file string) error {
	f, err := os.Open(sums)
	if err != nil {
		return err
	}
	defer f.Close()
	want := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == filepath.Base(file) {
			want = fields[0]
		}
	}
	if want == "" {
		return fmt.Errorf("%s isn't in checksums.txt", filepath.Base(file))
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != want {
		return fmt.Errorf("%s's checksum doesn't match checksums.txt: nothing was installed", filepath.Base(file))
	}
	return nil
}

func programName(p string) string {
	if goruntime.GOOS == "windows" {
		return p + ".exe"
	}
	return p
}

// extractPrograms takes blitz and blitzd out of the archive into dir.
func extractPrograms(archive, dir string) (map[string]string, error) {
	want := map[string]string{programName("blitz"): "blitz", programName("blitzd"): "blitzd"}
	out := map[string]string{}
	save := func(name string, r io.Reader) error {
		prog, ok := want[filepath.Base(name)]
		if !ok {
			return nil
		}
		dst := filepath.Join(dir, "new-"+filepath.Base(name))
		f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		_, err = io.Copy(f, io.LimitReader(r, 512<<20))
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		out[prog] = dst
		return err
	}
	if strings.HasSuffix(archive, ".zip") {
		zr, err := zip.OpenReader(archive)
		if err != nil {
			return nil, err
		}
		defer zr.Close()
		for _, f := range zr.File {
			if !f.Mode().IsRegular() {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			err = save(f.Name, rc)
			rc.Close()
			if err != nil {
				return nil, err
			}
		}
		return out, nil
	}
	f, err := os.Open(archive)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg {
			if err := save(h.Name, tr); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

// replaceFiles puts each source over its destination ({src, dst} pairs),
// all of them or none: every new file is staged beside its destination
// first, then renamed over it (a running program's file can be replaced
// so). The old files are kept until all are in place, and put back if one
// can't be.
func replaceFiles(pairs [][2]string) (err error) {
	var staged []string
	defer func() {
		for _, s := range staged {
			os.Remove(s) // only left on failure
		}
	}()
	for _, p := range pairs {
		data, err := os.ReadFile(p[0])
		if err != nil {
			return err
		}
		s := p[1] + ".new"
		if err := os.WriteFile(s, data, 0o755); err != nil {
			return fmt.Errorf("replacing %s: %w", p[1], err)
		}
		staged = append(staged, s)
	}
	type placed struct {
		dst string
		old bool // dst existed: its file is at dst.old
	}
	var done []placed
	defer func() {
		for i := len(done) - 1; i >= 0; i-- {
			d := done[i]
			switch {
			case err == nil && d.old:
				os.Remove(d.dst + ".old") // fails on Windows while it runs; the next update removes it
			case err != nil && d.old:
				os.Rename(d.dst+".old", d.dst)
			case err != nil:
				os.Remove(d.dst)
			}
		}
	}()
	for i, p := range pairs {
		dst := p[1]
		old, err := keepOld(dst)
		if err != nil {
			return fmt.Errorf("replacing %s: %w", dst, err)
		}
		if err := os.Rename(staged[i], dst); err != nil {
			if old {
				os.Rename(dst+".old", dst)
			}
			return fmt.Errorf("replacing %s: %w", dst, err)
		}
		done = append(done, placed{dst, old})
	}
	staged = nil
	return nil
}

// keepOld keeps dst's file at dst.old until the update is done, and
// reports whether there was one. Elsewhere than Windows it's a second
// link, so dst never goes missing; on Windows, where a running program's
// file can't be replaced, it's moved aside.
func keepOld(dst string) (bool, error) {
	old := dst + ".old"
	switch fi, err := os.Stat(dst); {
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	case err != nil:
		return false, err
	case fi.IsDir():
		return false, fmt.Errorf("%s is a folder", dst)
	}
	os.Remove(old)
	if goruntime.GOOS != "windows" && os.Link(dst, old) == nil {
		return true, nil
	}
	if err := os.Rename(dst, old); err != nil {
		return false, err
	}
	return true, nil
}

// compareVersions compares two versions (v1.2.3, v1.2.3-rc.1): negative
// when a is older, positive when newer. A pre-release is older than its
// release.
func compareVersions(a, b string) int {
	pa, prea := splitVersion(a)
	pb, preb := splitVersion(b)
	for i := range 3 {
		if pa[i] != pb[i] {
			return pa[i] - pb[i]
		}
	}
	switch {
	case prea == preb:
		return 0
	case prea == "":
		return 1
	case preb == "":
		return -1
	}
	return strings.Compare(prea, preb)
}

func splitVersion(v string) ([3]int, string) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	core, pre, _ := strings.Cut(v, "-")
	var out [3]int
	for i, part := range strings.SplitN(core, ".", 3) {
		out[i], _ = strconv.Atoi(part)
	}
	return out, pre
}
