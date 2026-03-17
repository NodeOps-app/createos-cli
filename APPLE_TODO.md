# Apple Notarization TODO

To distribute the CreateOS CLI on macOS without Gatekeeper warnings, complete the following steps.

## Prerequisites

- [ ] Enroll in the Apple Developer Program at https://developer.apple.com — $99/year
- [ ] Generate a **Developer ID Application** certificate in Xcode or at developer.apple.com/account
- [ ] Export the certificate as a `.p12` file and note the password

## Steps

### 1. Install GoReleaser

```bash
brew install goreleaser
```

### 2. Create `.goreleaser.yaml`

```yaml
builds:
  - env: [CGO_ENABLED=0]
    goos: [darwin, linux, windows]
    goarch: [amd64, arm64]
    ldflags:
      - -s -w -X github.com/NodeOps-app/createos-cli/internal/pkg/version.Version={{.Version}}

archives:
  - format: tar.gz
    name_template: "createos_{{ .Os }}_{{ .Arch }}"
    format_overrides:
      - goos: windows
        format: zip

signs:
  - cmd: codesign
    args:
      - "--sign"
      - "Developer ID Application: Your Name (TEAMID)"
      - "--options"
      - "runtime"
      - "--timestamp"
      - "${artifact}"
    artifacts: macos

notarize:
  macos:
    - enabled: true
      sign:
        certificate: "{{.Env.APPLE_CERT_BASE64}}"
        password: "{{.Env.APPLE_CERT_PASSWORD}}"
      notarize:
        issuer_id: "{{.Env.APPLE_ISSUER_ID}}"
        key_id: "{{.Env.APPLE_KEY_ID}}"
        key: "{{.Env.APPLE_KEY}}"
```

### 3. Set environment variables (CI or local)

| Variable | Description |
|----------|-------------|
| `APPLE_CERT_BASE64` | Base64-encoded `.p12` certificate (`base64 -i cert.p12`) |
| `APPLE_CERT_PASSWORD` | Password for the `.p12` file |
| `APPLE_ISSUER_ID` | Found in App Store Connect → Keys → Issuer ID |
| `APPLE_KEY_ID` | Found in App Store Connect → Keys |
| `APPLE_KEY` | Contents of the `.p8` API key file |

### 4. Manual signing (without GoReleaser)

```bash
# Sign
codesign --sign "Developer ID Application: Your Name (TEAMID)" \
  --options runtime \
  --timestamp \
  createos

# Zip for notarization submission
zip createos.zip createos

# Submit to Apple
xcrun notarytool submit createos.zip \
  --apple-id "you@email.com" \
  --team-id "YOURTEAMID" \
  --password "app-specific-password" \
  --wait

# Staple the notarization ticket to the binary
xcrun stapler staple createos
```

### 5. Verify

```bash
# Check signature
codesign --verify --verbose createos

# Check notarization
spctl --assess --verbose createos
# Expected: createos: accepted
```

## Notes

- `--options runtime` (hardened runtime) is required for notarization
- `--timestamp` uses Apple's secure timestamp server — required for notarization
- Stapling embeds the ticket so the binary works offline without calling Apple
- Without notarization, users see "Apple cannot verify this app" and must manually allow it in System Settings → Privacy & Security
- ARM (Apple Silicon) and AMD64 builds must both be signed separately
