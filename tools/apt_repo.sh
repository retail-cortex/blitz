#!/usr/bin/env bash
# Copyright 2026 Retail Cortex
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# The desktop app's apt repository (spec_025_release REL-30): one suite,
# stable, for every Ubuntu and Debian release the .deb installs on, with
# the latest release's packages, their AppStream data (what software
# centres show: the license, the homepage), and Release signed with the
# packages key. The release workflow uploads it to the bucket.
#   tools/apt_repo.sh <out> <key fingerprint> <deb>...
# Needs apt-ftparchive (apt-utils), gpg with the key, dpkg-deb and
# appstreamcli compose (appstream-compose), as on Ubuntu 24.04.
set -euo pipefail
out="$1" key="$2"
shift 2
[ $# -gt 0 ] || { echo "usage: $0 <out> <key fingerprint> <deb>..." >&2; exit 2; }

suite="$out/dists/stable"
pool="pool/main/b/blitz-desktop"
mkdir -p "$out/$pool"
cp "$@" "$out/$pool/"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

archs=()
for deb in "$@"; do
	arch="$(dpkg-deb -f "$deb" Architecture)"
	archs+=("$arch")
	bin="$suite/main/binary-$arch"
	mkdir -p "$bin"
	(cd "$out" && apt-ftparchive --arch "$arch" packages "$pool") >"$bin/Packages"
	gzip -9nkf "$bin/Packages"

	# AppStream: the package's metainfo, composed as apt fetches it
	# (main/dep11/Components-<arch>.yml.gz and the icons beside it).
	dpkg-deb -x "$deb" "$work/tree-$arch"
	# Compose writes the XML catalog (AppStream 1.0); apt fetches YAML.
	appstreamcli compose --no-net --origin=blitz --print-report=on-error \
		--result-root="$work/compose-$arch" --data-dir="$work/data-$arch" --icons-dir="$work/icons-$arch" \
		"$work/tree-$arch"
	gunzip -c "$work/data-$arch/blitz.xml.gz" >"$work/blitz-$arch.xml"
	appstreamcli convert --format=yaml "$work/blitz-$arch.xml" "$work/Components-$arch.yml"
	# The header's origin, which the conversion drops: AppStream finds the
	# icons by it.
	sed -i "0,/^Version: .*/s//&\nOrigin: blitz/" "$work/Components-$arch.yml"
	mkdir -p "$suite/main/dep11"
	# Each beside its .gz: apt fetches DEP-11 only when Release lists the
	# file it keeps, not just a compressed form (as Ubuntu's archive does).
	cp "$work/Components-$arch.yml" "$suite/main/dep11/"
	gzip -9nkf "$suite/main/dep11/Components-$arch.yml"
	for size in "$work/icons-$arch"/*; do
		[ -d "$size" ] || continue
		tar -C "$size" -cf "$suite/main/dep11/icons-$(basename "$size").tar" .
		gzip -9nkf "$suite/main/dep11/icons-$(basename "$size").tar"
	done
done

apt-ftparchive \
	-o APT::FTPArchive::Release::Origin="Retail Cortex" \
	-o APT::FTPArchive::Release::Label="Blitz" \
	-o APT::FTPArchive::Release::Suite=stable \
	-o APT::FTPArchive::Release::Codename=stable \
	-o APT::FTPArchive::Release::Components=main \
	-o APT::FTPArchive::Release::Architectures="$(printf '%s\n' "${archs[@]}" | sort -u | tr '\n' ' ' | sed 's/ $//')" \
	-o APT::FTPArchive::Release::Description="The Blitz desktop app" \
	release "$suite" >"$work/Release"
mv "$work/Release" "$suite/Release"
gpg --batch --yes --pinentry-mode loopback ${APT_SIGNING_PASSPHRASE:+--passphrase-fd 0} \
	--local-user "$key" --clearsign --digest-algo SHA512 -o "$suite/InRelease" "$suite/Release" <<<"${APT_SIGNING_PASSPHRASE:-}"
gpg --batch --yes --pinentry-mode loopback ${APT_SIGNING_PASSPHRASE:+--passphrase-fd 0} \
	--local-user "$key" --armor --detach-sign --digest-algo SHA512 -o "$suite/Release.gpg" "$suite/Release" <<<"${APT_SIGNING_PASSPHRASE:-}"
gpg --armor --export "$key" >"$out/blitz-desktop.asc"
echo "apt repository in $out: ${archs[*]}"
