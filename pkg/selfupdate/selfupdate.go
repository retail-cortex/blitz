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

// Package selfupdate finds, checks and installs Blitz's releases, for
// `blitz update` and for the desktop app updating itself: the latest
// release from GitHub, versions compared, a release's files downloaded
// and checked against its checksums.txt and that file's signature (the
// release workflow's, made keylessly with Sigstore, checked here without
// cosign), then put in place all at once or not at all.
package selfupdate

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
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
)

// Source is where releases are: GitHub's API and its downloads.
type Source struct {
	API      string // …/repos/<owner>/<repo>
	Download string // …/releases/download
}

// GitHub is Blitz's releases. BLITZ_UPDATE_URL (a folder served with
// api/releases/latest and download/<tag>/<file>) replaces it, to try an
// update against a local release.
func GitHub() Source {
	if u := strings.TrimRight(os.Getenv("BLITZ_UPDATE_URL"), "/"); u != "" {
		return Source{API: u + "/api", Download: u + "/download"}
	}
	return Source{API: "https://api.github.com/repos/retail-cortex/blitz", Download: "https://github.com/retail-cortex/blitz/releases/download"}
}

// File is a release's file's address.
func (s Source) File(tag, name string) string { return s.Download + "/" + tag + "/" + name }

// Latest is the latest release's tag (GitHub's latest: no drafts or
// pre-releases).
func (s Source) Latest(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.API+"/releases/latest", nil)
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

// PackageManager names what installed exe, if a package manager did:
// it updates it, not Blitz.
func PackageManager(exe string) string {
	switch {
	case strings.Contains(exe, "/Cellar/") || strings.Contains(exe, "/homebrew/"):
		return "Homebrew (brew upgrade blitz)"
	case strings.HasPrefix(exe, "/usr/bin/") || strings.HasPrefix(exe, "/usr/lib/"):
		return "your system's package manager"
	}
	return ""
}

// Fetch downloads url to the file to (512 MB at most).
func Fetch(ctx context.Context, url, to string) error {
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

// CheckSum checks file against its line in sums (checksums.txt).
func CheckSum(sums, file string) error {
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

// ProgramName is a program's file name here (.exe on Windows).
func ProgramName(p string) string {
	if goruntime.GOOS == "windows" {
		return p + ".exe"
	}
	return p
}

// ExtractPrograms takes the programs named (blitz, blitzd: without .exe)
// out of a release archive (.tar.gz, or .zip) into dir, wherever they are
// in it; it returns each one's file there.
func ExtractPrograms(archive, dir string, programs ...string) (map[string]string, error) {
	want := map[string]string{}
	for _, p := range programs {
		want[ProgramName(p)] = p
	}
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

// ReplaceFiles puts each source over its destination ({src, dst} pairs),
// all of them or none: every new file is staged beside its destination
// first, then renamed over it (a running program's file can be replaced
// so). The old files are kept until all are in place, and put back if one
// can't be.
func ReplaceFiles(pairs [][2]string) (err error) {
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

// Compare compares two versions (v1.2.3, v1.2.3-rc.1): negative
// when a is older, positive when newer. A pre-release is older than its
// release.
func Compare(a, b string) int {
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

// FileExists reports whether p exists.
func FileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
