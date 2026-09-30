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

// Command vsix packs a VS Code extension (apps/vscode) into a .vsix: the
// zip VS Code installs, with the manifest and content types vsce would
// write, the extension's files under extension/.
//
//	vsix --package apps/vscode/package.json --out blitz.vsix \
//	    --add extension/dist=<dir> --add extension/README.md=<file> …
package main

import (
	"archive/zip"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"mime"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// manifest is what the vsix needs from package.json.
type manifest struct {
	Name        string            `json:"name"`
	DisplayName string            `json:"displayName"`
	Description string            `json:"description"`
	Publisher   string            `json:"publisher"`
	Version     string            `json:"version"`
	Categories  []string          `json:"categories"`
	Engines     map[string]string `json:"engines"`
	Repository  struct {
		URL string `json:"url"`
	} `json:"repository"`
}

// adds are --add flags: archive path = source file or directory.
type adds []string

func (a *adds) String() string     { return strings.Join(*a, ",") }
func (a *adds) Set(s string) error { *a = append(*a, s); return nil }

func main() {
	pkg := flag.String("package", "", "the extension's package.json")
	out := flag.String("out", "", "the .vsix to write")
	version := flag.String("version", "", "the version to give it (default: package.json's)")
	var files adds
	flag.Var(&files, "add", "archive/path=source (a file, or a directory added under that path; repeatable)")
	flag.Parse()
	if err := run(*pkg, *out, *version, files); err != nil {
		fmt.Fprintln(os.Stderr, "vsix:", err)
		os.Exit(1)
	}
}

// entry is one file of the archive.
type entry struct {
	name string // in the archive
	data []byte
}

func run(pkgPath, out, version string, add adds) error {
	if pkgPath == "" || out == "" {
		return errors.New("--package and --out are required")
	}
	pkg, err := os.ReadFile(pkgPath)
	if err != nil {
		return err
	}
	if version != "" {
		pkg = setVersion(pkg, strings.TrimPrefix(version, "v"))
	}
	var m manifest
	if err := json.Unmarshal(pkg, &m); err != nil {
		return fmt.Errorf("%s: %w", pkgPath, err)
	}
	if m.Name == "" || m.Publisher == "" || m.Version == "" || m.Engines["vscode"] == "" {
		return fmt.Errorf("%s: name, publisher, version and engines.vscode are required", pkgPath)
	}
	entries := []entry{{"extension/package.json", pkg}}
	for _, a := range add {
		dst, src, ok := strings.Cut(a, "=")
		if !ok {
			return fmt.Errorf("--add %q: want archive/path=source", a)
		}
		more, err := collect(dst, src)
		if err != nil {
			return err
		}
		entries = append(entries, more...)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	for i := 1; i < len(entries); i++ {
		if entries[i].name == entries[i-1].name {
			return fmt.Errorf("%s is added twice", entries[i].name)
		}
	}
	vsixManifest, err := vsixManifest(m, entries)
	if err != nil {
		return err
	}
	all := append([]entry{{"extension.vsixmanifest", vsixManifest}, {"[Content_Types].xml", contentTypes(entries)}}, entries...)
	return write(out, all)
}

var versionField = regexp.MustCompile(`("version"\s*:\s*)"[^"]*"`)

// setVersion replaces package.json's first "version", keeping the rest as
// written.
func setVersion(pkg []byte, v string) []byte {
	done := false
	return versionField.ReplaceAllFunc(pkg, func(b []byte) []byte {
		if done {
			return b
		}
		done = true
		return versionField.ReplaceAll(b, []byte(`${1}"`+v+`"`))
	})
}

// collect reads src (a file, or a directory's files) as entries under dst.
func collect(dst, src string) ([]entry, error) {
	info, err := os.Stat(src)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		b, err := os.ReadFile(src)
		return []entry{{dst, b}}, err
	}
	var out []entry
	err = filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		b, err := os.ReadFile(p)
		out = append(out, entry{path.Join(dst, filepath.ToSlash(rel)), b})
		return err
	})
	return out, err
}

// has reports whether the archive has a file.
func has(entries []entry, name string) bool {
	for _, e := range entries {
		if e.name == name {
			return true
		}
	}
	return false
}

// vsixManifest is the package's extension.vsixmanifest.
func vsixManifest(m manifest, entries []entry) ([]byte, error) {
	type property struct {
		ID    string `xml:"Id,attr"`
		Value string `xml:"Value,attr"`
	}
	type asset struct {
		Type        string `xml:"Type,attr"`
		Path        string `xml:"Path,attr"`
		Addressable bool   `xml:"Addressable,attr"`
	}
	type doc struct {
		XMLName  xml.Name `xml:"PackageManifest"`
		Version  string   `xml:"Version,attr"`
		NS       string   `xml:"xmlns,attr"`
		Metadata struct {
			Identity struct {
				Language  string `xml:"Language,attr"`
				ID        string `xml:"Id,attr"`
				Version   string `xml:"Version,attr"`
				Publisher string `xml:"Publisher,attr"`
			}
			DisplayName string
			Description struct {
				Space string `xml:"xml:space,attr"`
				Text  string `xml:",chardata"`
			}
			Categories   string
			GalleryFlags string
			Properties   struct {
				Property []property
			}
			License string `xml:",omitempty"`
		}
		Installation struct {
			InstallationTarget struct {
				ID string `xml:"Id,attr"`
			}
		}
		Dependencies struct{}
		Assets       struct {
			Asset []asset
		}
	}
	var d doc
	d.Version, d.NS = "2.0.0", "http://schemas.microsoft.com/developer/vsx-schema/2011"
	md := &d.Metadata
	md.Identity.Language, md.Identity.ID, md.Identity.Version, md.Identity.Publisher = "en-US", m.Name, m.Version, m.Publisher
	md.DisplayName = cmpOr(m.DisplayName, m.Name)
	md.Description.Space, md.Description.Text = "preserve", m.Description
	md.Categories = strings.Join(m.Categories, ",")
	md.GalleryFlags = "Public"
	md.Properties.Property = []property{
		{"Microsoft.VisualStudio.Code.Engine", m.Engines["vscode"]},
		{"Microsoft.VisualStudio.Code.ExtensionDependencies", ""},
		{"Microsoft.VisualStudio.Code.ExtensionPack", ""},
		// It reaches the service's socket where the files are.
		{"Microsoft.VisualStudio.Code.ExtensionKind", "workspace"},
	}
	if m.Repository.URL != "" {
		md.Properties.Property = append(md.Properties.Property, property{"Microsoft.VisualStudio.Services.Links.Source", m.Repository.URL})
	}
	d.Installation.InstallationTarget.ID = "Microsoft.VisualStudio.Code"
	d.Assets.Asset = []asset{{"Microsoft.VisualStudio.Code.Manifest", "extension/package.json", true}}
	if has(entries, "extension/README.md") {
		d.Assets.Asset = append(d.Assets.Asset, asset{"Microsoft.VisualStudio.Services.Content.Details", "extension/README.md", true})
	}
	if has(entries, "extension/LICENSE.txt") {
		md.License = "extension/LICENSE.txt"
		d.Assets.Asset = append(d.Assets.Asset, asset{"Microsoft.VisualStudio.Services.Content.License", "extension/LICENSE.txt", true})
	}
	b, err := xml.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), b...), nil
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// contentTypes is [Content_Types].xml: a type for each file extension.
func contentTypes(entries []entry) []byte {
	types := map[string]string{".vsixmanifest": "text/xml", ".json": "application/json"}
	for _, e := range entries {
		ext := strings.ToLower(path.Ext(e.name))
		if ext == "" || types[ext] != "" {
			continue
		}
		t := mime.TypeByExtension(ext)
		if i := strings.IndexByte(t, ';'); i >= 0 {
			t = t[:i]
		}
		types[ext] = cmpOr(t, "application/octet-stream")
	}
	exts := make([]string, 0, len(types))
	for ext := range types {
		exts = append(exts, ext)
	}
	sort.Strings(exts)
	var sb strings.Builder
	sb.WriteString(xml.Header + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">`)
	for _, ext := range exts {
		fmt.Fprintf(&sb, `<Default Extension="%s" ContentType="%s"/>`, ext, types[ext])
	}
	sb.WriteString("</Types>\n")
	return []byte(sb.String())
}

// write makes the zip, its entries with a fixed time so builds match.
func write(out string, entries []entry) error {
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(f)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		h.Modified = fixedTime
		w, err := zw.CreateHeader(h)
		if err == nil {
			_, err = w.Write(e.data)
		}
		if err != nil {
			f.Close()
			return err
		}
	}
	if err := zw.Close(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// fixedTime stamps every entry (the zip format's earliest date).
var fixedTime = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)
