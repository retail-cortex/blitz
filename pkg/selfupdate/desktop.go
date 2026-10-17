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
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Install is the desktop app's installation, as its host finds it from
// its own program (…/resources/app/bin/blitz-tray, spec_electron_038).
type Install struct {
	// Root is what an update replaces: Blitz.app on macOS, the app's
	// folder elsewhere.
	Root string
	// Program is what starts the app again afterwards.
	Program string
	// Manager names the package manager that installed it, which updates
	// it instead ("" when the app updates itself).
	Manager string
}

// ErrNotInstalled is a host that isn't in an installed desktop app (a
// development build, run from Bazel).
var ErrNotInstalled = errors.New("this isn't an installed desktop app (a development build?): it doesn't update itself")

// FindInstall is the installation whose host program is hostExe, on goos.
func FindInstall(hostExe, goos string) (Install, error) {
	bin := filepath.Dir(hostExe)
	app := filepath.Dir(bin)
	res := filepath.Dir(app)
	if filepath.Base(bin) != "bin" || filepath.Base(app) != "app" || !strings.EqualFold(filepath.Base(res), "resources") {
		return Install{}, ErrNotInstalled
	}
	switch goos {
	case "darwin":
		root := filepath.Dir(filepath.Dir(res)) // Blitz.app/Contents/Resources
		if !strings.HasSuffix(root, ".app") {
			return Install{}, ErrNotInstalled
		}
		return Install{Root: root, Program: root}, nil
	case "windows":
		root := filepath.Dir(res)
		return Install{Root: root, Program: filepath.Join(root, "Blitz.exe")}, nil
	}
	root := filepath.Dir(res)
	in := Install{Root: root, Program: filepath.Join(root, "blitz-desktop")}
	if strings.HasPrefix(root, "/usr/") || strings.HasPrefix(root, "/opt/") {
		in.Manager = "your system's package manager"
	}
	return in, nil
}

// DesktopAsset is the release's desktop package for goos and goarch at
// version (no v): the one the app updates itself from.
func DesktopAsset(goos, goarch, version string) (string, error) {
	arch := map[string]string{"amd64": "x64", "arm64": "arm64"}[goarch]
	switch {
	case goos == "darwin" && arch != "":
		return fmt.Sprintf("Blitz_%s_macos_%s.dmg", version, arch), nil
	case goos == "windows" && goarch == "amd64":
		return fmt.Sprintf("Blitz_%s_windows_x64.zip", version), nil
	case goos == "linux" && arch != "":
		return fmt.Sprintf("blitz-desktop_%s_linux_%s.tar.gz", version, goarch), nil
	}
	return "", fmt.Errorf("no desktop package for %s/%s", goos, goarch)
}

// VerifyPackage checks a desktop package's signature (VerifySignature);
// tests, here and in the packages that stage updates, replace it.
var VerifyPackage = VerifySignature

// unpackDMG copies Blitz.app out of a disk image (hdiutil, macOS's); tests
// replace it.
var unpackDMG = hdiutilCopy

// Stage downloads tag's desktop package for goos/goarch into dir, checks it
// against its own signature (the release workflow's), and unpacks it there:
// it returns the new Blitz.app, or the app's folder, ready for Apply.
// cacheDir keeps Sigstore's trust root.
func Stage(ctx context.Context, src Source, tag, goos, goarch, dir, cacheDir string) (string, error) {
	name, err := DesktopAsset(goos, goarch, strings.TrimPrefix(tag, "v"))
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	pkg := filepath.Join(dir, name)
	for _, f := range []string{name, name + ".sigstore.json"} {
		if err := Fetch(ctx, src.File(tag, f), filepath.Join(dir, f)); err != nil {
			return "", fmt.Errorf("downloading %s: %w", f, err)
		}
	}
	bundle, err := os.ReadFile(pkg + ".sigstore.json")
	if err != nil {
		return "", err
	}
	f, err := os.Open(pkg)
	if err != nil {
		return "", err
	}
	err = VerifyPackage(ctx, f, bundle, cacheDir)
	f.Close()
	if err != nil {
		return "", fmt.Errorf("%w: nothing was installed", err)
	}
	out := filepath.Join(dir, "new")
	if err := os.RemoveAll(out); err != nil {
		return "", err
	}
	switch {
	case strings.HasSuffix(name, ".dmg"):
		if err := os.MkdirAll(out, 0o755); err != nil {
			return "", err
		}
		return filepath.Join(out, "Blitz.app"), unpackDMG(ctx, pkg, filepath.Join(out, "Blitz.app"))
	case strings.HasSuffix(name, ".zip"):
		return filepath.Join(out, "Blitz"), unzip(pkg, out)
	}
	return filepath.Join(out, "blitz-desktop"), untar(pkg, out)
}

// command runs a program and returns what it printed; tests replace it.
var command = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// hdiutilCopy copies Blitz.app out of a disk image (mounted where nothing
// shows it, then let go) to dst.
func hdiutilCopy(ctx context.Context, dmg, dst string) error {
	mnt, err := os.MkdirTemp("", "blitz-update-")
	if err != nil {
		return err
	}
	defer os.Remove(mnt)
	if out, err := command(ctx, "hdiutil", "attach", "-nobrowse", "-readonly", "-noautoopen", "-mountpoint", mnt, dmg); err != nil {
		return fmt.Errorf("opening the disk image: %w: %s", err, strings.TrimSpace(string(out)))
	}
	defer command(context.WithoutCancel(ctx), "hdiutil", "detach", "-force", mnt)
	if out, err := command(ctx, "ditto", filepath.Join(mnt, "Blitz.app"), dst); err != nil {
		return fmt.Errorf("copying Blitz.app: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// inside is where name goes under dir, or an error for a name that would
// leave it (absolute, or climbing out with ..).
func inside(dir, name string) (string, error) {
	clean := filepath.Clean(filepath.FromSlash(name))
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.VolumeName(clean) != "" {
		return "", fmt.Errorf("the package has a file outside it: %s", name)
	}
	return filepath.Join(dir, clean), nil
}

// unzip unpacks a zip into dir.
func unzip(archive, dir string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		dst, err := inside(dir, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return err
			}
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		err = writeFile(dst, rc, f.Mode().Perm()|0o600)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// untar unpacks a .tar.gz into dir: files, folders and links (which must
// stay inside it).
func untar(archive, dir string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		dst, err := inside(dir, h.Name)
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dst, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			// Without root, a setuid bit can't be kept: the permissions only.
			if err := writeFile(dst, tr, os.FileMode(h.Mode).Perm()|0o600); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if _, err := inside(dir, filepath.Join(filepath.Dir(h.Name), h.Linkname)); err != nil || filepath.IsAbs(h.Linkname) {
				return fmt.Errorf("the package has a link outside it: %s", h.Name)
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(h.Linkname, dst); err != nil {
				return err
			}
		}
	}
}

// writeFile writes r to path (its folder made), 2 GB at most.
func writeFile(path string, r io.Reader, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, io.LimitReader(r, 2<<30))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// Apply puts the staged app in root's place: root moved aside (root.old),
// the staged one moved in, and root put back if that fails. Across disks,
// where a move can't be, the staged app is copied in. The old one is
// removed afterwards (a file still open keeps it on Windows until next
// time).
func Apply(staged, root string) error {
	if _, err := os.Stat(staged); err != nil {
		return fmt.Errorf("the update isn't there: %w", err)
	}
	old := root + ".old"
	if err := os.RemoveAll(old); err != nil {
		return err
	}
	if err := os.Rename(root, old); err != nil {
		return fmt.Errorf("moving the app aside: %w", err)
	}
	if err := os.Rename(staged, root); err != nil {
		if cerr := copyTree(staged, root); cerr != nil {
			os.RemoveAll(root)
			if rerr := os.Rename(old, root); rerr != nil {
				return fmt.Errorf("putting the update in place: %w (and the app couldn't be put back from %s: %v)", cerr, old, rerr)
			}
			return fmt.Errorf("putting the update in place: %w", cerr)
		}
	}
	os.RemoveAll(old)
	return nil
}

// copyTree copies src to dst: folders, files with their permissions, and
// links as links.
func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case info.IsDir():
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		return writeFile(target, f, info.Mode().Perm())
	})
}
