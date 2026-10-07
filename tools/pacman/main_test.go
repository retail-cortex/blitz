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
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// item is one entry of a test tar.
type item struct {
	name, body, link string
	typ              byte
	mode             int64
}

// payload is a tar of items, as pkg_tar writes it.
func payload(t *testing.T, items ...item) []byte {
	t.Helper()
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	for _, it := range items {
		h := &tar.Header{Name: it.name, Typeflag: it.typ, Mode: it.mode, Linkname: it.link, Size: int64(len(it.body)), Uid: 1000, Uname: "builder"}
		require.NoError(t, tw.WriteHeader(h))
		_, err := tw.Write([]byte(it.body))
		require.NoError(t, err)
	}
	require.NoError(t, tw.Close())
	return b.Bytes()
}

// got is a package's entries, in order, and their headers and bodies.
type got struct {
	names   []string
	headers map[string]*tar.Header
	bodies  map[string]string
}

func untar(t *testing.T, b []byte) got {
	t.Helper()
	g := got{headers: map[string]*tar.Header{}, bodies: map[string]string{}}
	tr := tar.NewReader(bytes.NewReader(b))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return g
		}
		require.NoError(t, err)
		body, err := io.ReadAll(tr)
		require.NoError(t, err)
		g.names = append(g.names, h.Name)
		g.headers[h.Name], g.bodies[h.Name] = h, string(body)
	}
}

func gunzip(t *testing.T, s string) string {
	t.Helper()
	zr, err := gzip.NewReader(strings.NewReader(s))
	require.NoError(t, err)
	b, err := io.ReadAll(zr)
	require.NoError(t, err)
	return string(b)
}

var desktop = []item{
	{name: "./usr/lib/blitz-desktop/blitz", body: "#!cli", typ: tar.TypeReg, mode: 0o755},
	{name: "./usr/lib/blitz-desktop/blitz-desktop", body: "#!app", typ: tar.TypeReg, mode: 0o755},
	{name: "usr/bin/blitz-desktop", link: "../lib/blitz-desktop/blitz-desktop", typ: tar.TypeSymlink, mode: 0o777},
	{name: "usr/share/licenses/", typ: tar.TypeDir, mode: 0o755},
	{name: "usr/share/licenses/blitz-desktop/NOTICE", body: "Blitz", typ: tar.TypeReg, mode: 0o644},
}

func TestRun(t *testing.T) {
	dir := t.TempDir()
	data, version, out := filepath.Join(dir, "data.tar"), filepath.Join(dir, "version.txt"), filepath.Join(dir, "pkg.tar")
	require.NoError(t, os.WriteFile(data, payload(t, desktop...), 0o644))
	require.NoError(t, os.WriteFile(version, []byte("0.4.0-rc1\n"), 0o644))
	m := meta{
		name: "blitz-desktop", arch: "x86_64", desc: "Blitz desktop app", url: "https://example.com",
		licenses: list{"Apache-2.0"}, depends: list{"bubblewrap", "webkit2gtk-4.1>=2.40"},
	}
	t.Setenv("SOURCE_DATE_EPOCH", "1700000000")
	require.NoError(t, run(m, data, version, out))
	b, err := os.ReadFile(out)
	require.NoError(t, err)
	g := untar(t, b)

	t.Run("metadata first, then the payload sorted, with its parents", func(t *testing.T) {
		assert.Equal(t, []string{
			".PKGINFO", ".MTREE",
			"usr/", "usr/bin/", "usr/bin/blitz-desktop", "usr/lib/", "usr/lib/blitz-desktop/",
			"usr/lib/blitz-desktop/blitz", "usr/lib/blitz-desktop/blitz-desktop",
			"usr/share/", "usr/share/licenses/", "usr/share/licenses/blitz-desktop/",
			"usr/share/licenses/blitz-desktop/NOTICE",
		}, g.names)
	})

	t.Run(".PKGINFO", func(t *testing.T) {
		info := g.bodies[".PKGINFO"]
		for _, l := range []string{
			"pkgname = blitz-desktop", "pkgbase = blitz-desktop", "pkgver = 0.4.0rc1-1",
			"pkgdesc = Blitz desktop app", "url = https://example.com", "builddate = 1700000000",
			"packager = Unknown Packager", "size = 15", "arch = x86_64", "license = Apache-2.0",
			"depend = bubblewrap", "depend = webkit2gtk-4.1>=2.40",
		} {
			assert.Contains(t, info, l+"\n")
		}
	})

	t.Run("owned by root at the build's time", func(t *testing.T) {
		for name, h := range g.headers {
			assert.Equal(t, 0, h.Uid, name)
			assert.Equal(t, "root", h.Uname, name)
			assert.Equal(t, time.Unix(1700000000, 0).UTC(), h.ModTime.UTC(), name)
		}
	})

	t.Run("modes and links kept", func(t *testing.T) {
		assert.Equal(t, int64(0o755), g.headers["usr/lib/blitz-desktop/blitz"].Mode)
		assert.Equal(t, int64(0o644), g.headers["usr/share/licenses/blitz-desktop/NOTICE"].Mode)
		assert.Equal(t, int64(0o755), g.headers["usr/lib/"].Mode)
		assert.Equal(t, "../lib/blitz-desktop/blitz-desktop", g.headers["usr/bin/blitz-desktop"].Linkname)
	})

	t.Run(".MTREE", func(t *testing.T) {
		tree := gunzip(t, g.bodies[".MTREE"])
		assert.True(t, strings.HasPrefix(tree, "#mtree\n/set type=file uid=0 gid=0 mode=644\n"))
		for _, l := range []string{
			fmt.Sprintf("./.PKGINFO time=1700000000.0 size=%d md5digest=", len(g.bodies[".PKGINFO"])),
			"./usr/lib time=1700000000.0 mode=755 type=dir",
			"./usr/bin/blitz-desktop time=1700000000.0 mode=777 type=link link=../lib/blitz-desktop/blitz-desktop",
			fmt.Sprintf("./usr/lib/blitz-desktop/blitz time=1700000000.0 mode=755 size=5 md5digest=%x sha256digest=%x", md5.Sum([]byte("#!cli")), sha256.Sum256([]byte("#!cli"))),
			"./usr/share/licenses/blitz-desktop/NOTICE time=1700000000.0 size=5 ",
		} {
			assert.Contains(t, tree, l)
		}
		assert.NotContains(t, tree, ".MTREE")
	})

	t.Run("the same input makes the same package", func(t *testing.T) {
		again := filepath.Join(dir, "again.tar")
		require.NoError(t, run(m, data, version, again))
		b2, err := os.ReadFile(again)
		require.NoError(t, err)
		assert.Equal(t, b, b2)
	})
}

func TestRunErrors(t *testing.T) {
	dir := t.TempDir()
	version := filepath.Join(dir, "version.txt")
	require.NoError(t, os.WriteFile(version, []byte("1.0.0"), 0o644))
	n := 0
	tarOf := func(items ...item) string {
		n++
		p := filepath.Join(dir, fmt.Sprintf("data%d.tar", n))
		require.NoError(t, os.WriteFile(p, payload(t, items...), 0o644))
		return p
	}
	m := meta{name: "blitz-desktop", arch: "x86_64"}
	for _, tc := range []struct {
		name, data, version, want string
		m                         meta
	}{
		{"flags", "x", version, "required", meta{}},
		{"no version file", "x", filepath.Join(dir, "none"), "no such file", m},
		{"no payload", filepath.Join(dir, "none.tar"), version, "no such file", m},
		{"metadata in the payload", tarOf(item{name: ".PKGINFO", typ: tar.TypeReg}), version, "metadata", m},
		{"a hard link", tarOf(item{name: "a", typ: tar.TypeLink, link: "b"}, item{name: "b", typ: tar.TypeReg}), version, "unsupported", m},
		{"twice", tarOf(item{name: "a", typ: tar.TypeReg}, item{name: "./a", typ: tar.TypeReg}, item{name: "b", typ: tar.TypeReg}), version, "twice", m},
		{"not a tar", version, version, "", m},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := run(tc.m, tc.data, tc.version, filepath.Join(dir, "out.tar"))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestPkgver(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"0.3.2\n", "0.3.2"},
		{"v1.0.0", "1.0.0"},
		{"0.4.0-rc1", "0.4.0rc1"},
		{"0.4.0-rc.1", "0.4.0rc.1"},
		{"0.3.2-3-gabc123", "0.3.2.3gabc123"},
		{"1.0-", "1.0."},
		{"1.0+build_7", "1.0+build_7"},
		{"", "0.0.0"},
	} {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, pkgver(tc.in))
		})
	}
}

func TestBuildDate(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want time.Time
	}{
		{"1700000000", time.Unix(1700000000, 0).UTC()},
		{"", fixedTime},
		{"soon", fixedTime},
		{"0", fixedTime},
	} {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, buildDate(tc.in))
		})
	}
}

func TestEscape(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"usr/bin/blitz", "usr/bin/blitz"},
		{"a b", `a\040b`},
		{`a\b`, `a\134b`},
		{"#x", `\043x`},
		{"é", `\303\251`},
	} {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, escape(tc.in))
		})
	}
}
