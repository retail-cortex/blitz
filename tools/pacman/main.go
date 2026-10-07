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

// Command pacman makes an Arch Linux package from a payload tar (a
// pkg_tar rooted at /): the archive pacman installs, with the .PKGINFO
// and the .MTREE makepkg would write ahead of the payload, every entry
// owned by root, its parent directories added and its times fixed. It
// writes the tar uncompressed; the build compresses it with zstd into
// the .pkg.tar.zst (apps/desktop/packaging).
//
//	pacman --data payload.tar --version-file version.txt --arch x86_64 \
//	    --name blitz-desktop --desc … --url … --license Apache-2.0 \
//	    --packager … --depend bubblewrap --depend … --out package.tar
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

// list is a repeatable flag.
type list []string

func (l *list) String() string     { return strings.Join(*l, ",") }
func (l *list) Set(s string) error { *l = append(*l, s); return nil }

// meta is the package's .PKGINFO, but for what the payload gives.
type meta struct {
	name, version, arch, desc, url, packager string
	licenses, depends                        list
}

func main() {
	var m meta
	data := flag.String("data", "", "the payload tar, rooted at /")
	versionFile := flag.String("version-file", "", "a file holding the version (//build:version)")
	out := flag.String("out", "", "the package tar to write (uncompressed)")
	flag.StringVar(&m.name, "name", "", "pkgname")
	flag.StringVar(&m.arch, "arch", "", "arch: x86_64 or aarch64")
	flag.StringVar(&m.desc, "desc", "", "pkgdesc")
	flag.StringVar(&m.url, "url", "", "url")
	flag.StringVar(&m.packager, "packager", "", "packager")
	flag.Var(&m.licenses, "license", "an SPDX license (repeatable)")
	flag.Var(&m.depends, "depend", "a dependency, such as gtk3 or webkit2gtk-4.1>=2.40 (repeatable)")
	flag.Parse()
	if err := run(m, *data, *versionFile, *out); err != nil {
		fmt.Fprintln(os.Stderr, "pacman:", err)
		os.Exit(1)
	}
}

// entry is one entry of the payload.
type entry struct {
	name string // without a leading ./ or a trailing /
	hdr  *tar.Header
	data []byte
}

func run(m meta, data, versionFile, out string) error {
	if data == "" || versionFile == "" || out == "" || m.name == "" || m.arch == "" {
		return errors.New("--data, --version-file, --out, --name and --arch are required")
	}
	v, err := os.ReadFile(versionFile)
	if err != nil {
		return err
	}
	m.version = pkgver(string(v)) + "-1"
	f, err := os.Open(data)
	if err != nil {
		return err
	}
	defer f.Close()
	entries, err := read(f)
	if err != nil {
		return fmt.Errorf("%s: %w", data, err)
	}
	when := buildDate(os.Getenv("SOURCE_DATE_EPOCH"))
	w, err := os.Create(out)
	if err != nil {
		return err
	}
	if err := write(w, m, entries, when); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}

// pkgver makes a version pacman takes: no "-" (it separates pkgrel) and
// only letters, digits, ".", "_" and "+". A "-" before a letter goes, as
// in Arch's own pre-releases: 0.4.0-rc1 is 0.4.0rc1, which vercmp puts
// before 0.4.0 (with any separator, 0.4.0.rc1, it would come after).
// Anything else becomes ".", so 0.3.2-3-gabc is 0.3.2.3gabc, after 0.3.2.
func pkgver(v string) string {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "" {
		return "0.0.0"
	}
	letter := func(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case letter(c), c >= '0' && c <= '9', c == '.', c == '_', c == '+':
			b.WriteByte(c)
		case c == '-' && i+1 < len(v) && letter(v[i+1]):
		default:
			b.WriteByte('.')
		}
	}
	return b.String()
}

// buildDate is SOURCE_DATE_EPOCH's time, or a fixed one so builds match.
func buildDate(epoch string) time.Time {
	if s, err := strconv.ParseInt(epoch, 10, 64); err == nil && s > 0 {
		return time.Unix(s, 0).UTC()
	}
	return fixedTime
}

// fixedTime is rules_pkg's own default for an entry's time.
var fixedTime = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

// read reads the payload's files, links and directories, and adds the
// directories it leaves out, so the package owns them and pacman removes
// them with it.
func read(r io.Reader) ([]entry, error) {
	tr := tar.NewReader(r)
	byName := map[string]entry{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		name := strings.Trim(strings.TrimPrefix(path.Clean("/"+h.Name), "/"), "/")
		if name == "" {
			continue
		}
		if strings.HasPrefix(name, ".") && !strings.Contains(name, "/") {
			return nil, fmt.Errorf("%s: the payload can't hold the package's own metadata", name)
		}
		switch h.Typeflag {
		case tar.TypeReg, tar.TypeDir, tar.TypeSymlink:
		default:
			return nil, fmt.Errorf("%s: unsupported entry type %q", name, h.Typeflag)
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
		if _, dup := byName[name]; dup && h.Typeflag != tar.TypeDir {
			return nil, fmt.Errorf("%s is in the payload twice", name)
		}
		byName[name] = entry{name, h, b}
	}
	for name := range byName {
		for d := path.Dir(name); d != "."; d = path.Dir(d) {
			if _, ok := byName[d]; !ok {
				byName[d] = entry{d, &tar.Header{Typeflag: tar.TypeDir, Mode: 0o755}, nil}
			}
		}
	}
	out := make([]entry, 0, len(byName))
	for _, e := range byName {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out, nil
}

// pkginfo is the package's .PKGINFO.
func pkginfo(m meta, entries []entry, when time.Time) []byte {
	var size int64
	for _, e := range entries {
		size += int64(len(e.data))
	}
	var b bytes.Buffer
	b.WriteString("# Generated by //tools/pacman\n")
	line := func(k, v string) {
		if v != "" {
			fmt.Fprintf(&b, "%s = %s\n", k, v)
		}
	}
	line("pkgname", m.name)
	line("pkgbase", m.name)
	line("xdata", "pkgtype=pkg")
	line("pkgver", m.version)
	line("pkgdesc", m.desc)
	line("url", m.url)
	line("builddate", strconv.FormatInt(when.Unix(), 10))
	line("packager", cmpOr(m.packager, "Unknown Packager"))
	line("size", strconv.FormatInt(size, 10))
	line("arch", m.arch)
	for _, l := range m.licenses {
		line("license", l)
	}
	for _, d := range m.depends {
		line("depend", d)
	}
	return b.Bytes()
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// mtree is the package's .MTREE, gzipped, as makepkg writes it with
// bsdtar: each entry's type, mode, time, size and digests, for
// pacman -Qkk to check the installed files against.
func mtree(info []byte, entries []entry, when time.Time) ([]byte, error) {
	var b bytes.Buffer
	t := strconv.FormatInt(when.Unix(), 10) + ".0"
	b.WriteString("#mtree\n/set type=file uid=0 gid=0 mode=644\n")
	file := func(name string, mode int64, data []byte) {
		fmt.Fprintf(&b, "./%s time=%s", escape(name), t)
		if mode&0o7777 != 0o644 {
			fmt.Fprintf(&b, " mode=%o", mode&0o7777)
		}
		fmt.Fprintf(&b, " size=%d md5digest=%x sha256digest=%x\n", len(data), md5.Sum(data), sha256.Sum256(data))
	}
	file(".PKGINFO", 0o644, info)
	for _, e := range entries {
		switch e.hdr.Typeflag {
		case tar.TypeDir:
			fmt.Fprintf(&b, "./%s time=%s mode=%o type=dir\n", escape(e.name), t, e.hdr.Mode&0o7777)
		case tar.TypeSymlink:
			fmt.Fprintf(&b, "./%s time=%s mode=777 type=link link=%s\n", escape(e.name), t, escape(e.hdr.Linkname))
		default:
			file(e.name, e.hdr.Mode, e.data)
		}
	}
	var z bytes.Buffer
	zw := gzip.NewWriter(&z) // no name or time in the header, so builds match
	if _, err := zw.Write(b.Bytes()); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return z.Bytes(), nil
}

// escape writes a path as mtree does: anything but a printable,
// non-space character, and the backslash, as a backslash and three
// octal digits.
func escape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c <= ' ' || c >= 0x7f || c == '\\' || c == '#' {
			fmt.Fprintf(&b, "\\%03o", c)
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// write writes the package: .PKGINFO, .MTREE, then the payload, every
// entry owned by root and stamped with the build's time.
func write(w io.Writer, m meta, entries []entry, when time.Time) error {
	info := pkginfo(m, entries, when)
	tree, err := mtree(info, entries, when)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(w)
	put := func(h *tar.Header, data []byte) error {
		h.Uid, h.Gid, h.Uname, h.Gname, h.ModTime = 0, 0, "root", "root", when
		h.AccessTime, h.ChangeTime, h.PAXRecords, h.Format = time.Time{}, time.Time{}, nil, tar.FormatUnknown
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		_, err := tw.Write(data)
		return err
	}
	for _, meta := range []entry{
		{".PKGINFO", &tar.Header{Typeflag: tar.TypeReg, Mode: 0o644}, info},
		{".MTREE", &tar.Header{Typeflag: tar.TypeReg, Mode: 0o644}, tree},
	} {
		meta.hdr.Name, meta.hdr.Size = meta.name, int64(len(meta.data))
		if err := put(meta.hdr, meta.data); err != nil {
			return err
		}
	}
	for _, e := range entries {
		h := &tar.Header{Typeflag: e.hdr.Typeflag, Name: e.name, Mode: e.hdr.Mode & 0o7777}
		switch e.hdr.Typeflag {
		case tar.TypeDir:
			h.Name += "/"
		case tar.TypeSymlink:
			h.Linkname, h.Mode = e.hdr.Linkname, 0o777
		default:
			h.Size = int64(len(e.data))
		}
		if err := put(h, e.data); err != nil {
			return err
		}
	}
	return tw.Close()
}
