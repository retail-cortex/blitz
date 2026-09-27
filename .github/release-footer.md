### Verifying

`checksums.txt` lists every archive; its signature is `checksums.txt.sigstore.json`. Each desktop package has its own `.sigstore.json`. Verify against this repository's release workflow:

```bash
cosign verify-blob --bundle checksums.txt.sigstore.json \
  --certificate-identity-regexp '^https://github.com/retail-cortex/blitz/\.github/workflows/release\.yml@refs/tags/v' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com checksums.txt
sha256sum --ignore-missing -c checksums.txt
```

### Desktop app

`Blitz_*_macos_universal.dmg` (notarized when the release was signed) and `blitz-desktop_*.deb` (Ubuntu 24.04, Debian 13 and later).

### macOS

The macOS CLI binaries aren't notarized by Apple. If you download one in a browser, Gatekeeper refuses to run it until you clear the quarantine flag:

```bash
xattr -d com.apple.quarantine blitz blitzd
```

You can also open it once through Finder's context menu (Open). A copy downloaded with `curl` isn't quarantined.
