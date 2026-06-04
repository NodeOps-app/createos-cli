package sandbox

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// Mutagen version pinned by createos. Bump this when we want a newer
// engine across the install base; the on-disk cached binary at
// localMutagenPath() will be re-downloaded the next time someone runs
// `createos sync` without an upgraded binary on PATH.
const mutagenVersion = "v0.18.1"

// mutagenSHA256 pins the expected sha256 of each release archive we
// might download. Mirrors the official SHA256SUMS at
// https://github.com/mutagen-io/mutagen/releases/download/<ver>/SHA256SUMS.
// Without this we'd run whatever the CDN happens to serve — a MITM /
// account-takeover / cache-poison attacker could swap in a malicious
// binary and we'd execute it on the user's laptop. Keyed by the asset
// filename (NOT the full URL) so it's easy to diff against upstream.
//
// Bump mutagenVersion → also refresh these.
var mutagenSHA256 = map[string]string{
	"mutagen_linux_amd64_v0.18.1.tar.gz":  "7735286c778cc438418209f24d03a64f3a0151c8065ef0fe079cfaf093af6f8f",
	"mutagen_linux_arm64_v0.18.1.tar.gz":  "bcba735aebf8cbc11da9b3742118a665599ac697fa06bc5751cac8dcd540db8a",
	"mutagen_darwin_amd64_v0.18.1.tar.gz": "7d06f7d8fcfe90bc7e55cc834a2f2f20c2e0af9ea9bc35911fc4341ad56a9bbf",
	"mutagen_darwin_arm64_v0.18.1.tar.gz": "6f810416d9e5fc4fd5e18431146f8b3c5a2056ba5a24f76c1e66da86eb3257e2",
	"mutagen_windows_amd64_v0.18.1.zip":   "76f8223d5e6b607efdd9516473669ae5492e4f142887352d59bc6934d1f07a2d",
	"mutagen_windows_arm64_v0.18.1.zip":   "d0dd95b60f6077f0c02baee3128f754c1507bc4abfa63ae0bcae12e01a3d45f1",
}

// ensureMutagen returns an absolute path to a working `mutagen`
// binary. Resolution order:
//
//  1. mutagen already on $PATH — use it (lets users override the
//     pinned version with their own install, including newer / dev
//     builds).
//  2. mutagen previously downloaded by createos, sitting under
//     ~/.config/createos/bin (or ~/.createos/bin on Windows) — reuse it
//     without a network round-trip.
//  3. Otherwise: download the pinned version from GitHub releases for
//     the current GOOS/GOARCH, extract the binary, chmod 0755, cache
//     under (2)'s path, and return that.
//
// Side effect: prints a one-line progress note to stderr when it has
// to download, so the user knows what the brief delay is.
func ensureMutagen() (string, error) {
	if p, err := exec.LookPath("mutagen"); err == nil {
		return p, nil
	}
	dir, err := mutagenCacheDir()
	if err != nil {
		return "", err
	}
	target := filepath.Join(dir, mutagenBinaryName())
	if _, err = os.Stat(target); err == nil {
		return target, nil
	}
	if err = os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("create mutagen cache dir: %w", err)
	}
	url, ext, err := mutagenReleaseURL()
	if err != nil {
		return "", err
	}
	assetName := filepath.Base(url)
	expectedHash := mutagenSHA256[assetName]
	// First-time install message. Explains the why (we use mutagen as
	// the sync engine), the where (github release URL), and the
	// security check (sha256 pinned in source). One-time per host
	// across the user's lifetime of createos — after this the binary is
	// cached, no further downloads.
	fmt.Fprintln(os.Stderr, "createos sync uses Mutagen (https://mutagen.io) as the bidirectional sync engine.")
	fmt.Fprintf(os.Stderr, "First-time setup: downloading %s (%s/%s)\n", mutagenVersion, runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(os.Stderr, "  from: %s\n", url)
	fmt.Fprintf(os.Stderr, "  verifying against pinned sha256: %s…\n", expectedHash[:16])
	fmt.Fprintf(os.Stderr, "  caching at: %s\n", target)
	if err := downloadAndExtractMutagen(url, ext, target); err != nil {
		return "", err
	}
	fmt.Fprintln(os.Stderr, "createos sync: mutagen installed.")
	return target, nil
}

// mutagenReleaseURL returns the GitHub-release tarball URL for the
// current host and the archive extension ("tar.gz" / "zip").
func mutagenReleaseURL() (url, ext string, err error) {
	goos, goarch := runtime.GOOS, runtime.GOARCH
	supported := map[string]map[string]bool{
		"linux":   {"amd64": true, "arm64": true},
		"darwin":  {"amd64": true, "arm64": true},
		"windows": {"amd64": true, "arm64": true},
	}
	if !supported[goos][goarch] {
		return "", "", fmt.Errorf("no mutagen build for %s/%s — install it manually from https://mutagen.io",
			goos, goarch)
	}
	ext = "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	url = fmt.Sprintf(
		"https://github.com/mutagen-io/mutagen/releases/download/%s/mutagen_%s_%s_%s.%s",
		mutagenVersion, goos, goarch, mutagenVersion, ext)
	return url, ext, nil
}

// mutagenCacheDir is where createos drops the downloaded binary. We
// prefer XDG_CONFIG_HOME on unix and APPDATA on Windows; fall back to
// $HOME/.createos when neither is set (uncommon but happens in CI).
func mutagenCacheDir() (string, error) {
	if runtime.GOOS == "windows" {
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			return filepath.Join(appdata, "createos", "bin"), nil
		}
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "createos", "bin"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "createos", "bin"), nil
}

func mutagenBinaryName() string {
	if runtime.GOOS == "windows" {
		return "mutagen.exe"
	}
	return "mutagen"
}

// downloadAndExtractMutagen pulls the release archive, unpacks just
// the `mutagen` binary, and writes it to target with 0755. Streams
// through memory rather than to disk — the binary is ~30 MB
// compressed which is fine for a one-time download.
// mutagenAgentBundleName is the file mutagen searches for in its
// binary's directory to find per-platform remote agents. Without this
// alongside `mutagen` itself, sync sessions fail at
// "unable to locate agent bundle" right after the daemon dials the
// remote SSH endpoint.
const mutagenAgentBundleName = "mutagen-agents.tar.gz"

func downloadAndExtractMutagen(url, ext, target string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch %s: HTTP %d", url, resp.StatusCode)
	}
	// Stream the body through a progress reporter — prints bytes / total
	// to stderr on a throttled cadence so the user sees something moving
	// during the ~10-30 MiB download. Falls back to "X MiB" with no
	// percentage when Content-Length is unset.
	totalBytes := resp.ContentLength
	pr := &progressReader{r: resp.Body, total: totalBytes, label: "  downloading"}
	body, err := io.ReadAll(io.LimitReader(pr, 200<<20)) // 200 MiB ceiling
	pr.done()
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	// Integrity check: refuse to extract / install a binary that doesn't
	// match the pinned sha256 for this version+platform. Without this,
	// a compromised CDN or a MITM could swap the archive for something
	// arbitrary and we'd happily run it on the user's laptop.
	assetName := filepath.Base(url)
	expected, ok := mutagenSHA256[assetName]
	if !ok {
		return fmt.Errorf("no pinned sha256 for %s — refusing to install (bump mutagenSHA256 in mutagen_install.go)", assetName)
	}
	sum := sha256.Sum256(body)
	got := hex.EncodeToString(sum[:])
	if got != expected {
		return fmt.Errorf("sha256 mismatch for %s: expected %s, got %s — refusing to install (possible CDN compromise or version drift)",
			assetName, expected, got)
	}
	// Pull both the mutagen binary AND the agent bundle in one pass.
	// The bundle must live next to the binary; without it sync sessions
	// fail at "unable to locate agent bundle" the moment the daemon
	// tries to push the remote agent.
	var (
		bin, bundle []byte
		extractErr  error
	)
	switch ext {
	case "tar.gz":
		bin, bundle, extractErr = extractMutagenFromTarGz(body, mutagenBinaryName(), mutagenAgentBundleName)
	case "zip":
		bin, bundle, extractErr = extractMutagenFromZip(body, mutagenBinaryName(), mutagenAgentBundleName)
	default:
		extractErr = fmt.Errorf("unsupported archive ext %q", ext)
	}
	if extractErr != nil {
		return extractErr
	}
	if len(bin) == 0 {
		return fmt.Errorf("mutagen binary not found inside archive at %s", url)
	}
	if len(bundle) == 0 {
		return fmt.Errorf("%s not found inside archive at %s", mutagenAgentBundleName, url)
	}
	if err := os.WriteFile(target, bin, 0o755); err != nil { // #nosec G306 -- mutagen agent binary must be executable
		return fmt.Errorf("write %s: %w", target, err)
	}
	bundlePath := filepath.Join(filepath.Dir(target), mutagenAgentBundleName)
	if err := os.WriteFile(bundlePath, bundle, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", bundlePath, err)
	}
	return nil
}

// progressReader wraps an io.Reader, counts bytes through, and emits a
// throttled stderr line so the user sees download progress. Carriage-
// return rewrites the same line; done() prints a final newline.
//
// Throttle to one repaint per 100ms — fast enough to feel live, slow
// enough not to flood logs when stderr is a pipe.
type progressReader struct {
	r       io.Reader
	total   int64 // -1 if unknown
	read    int64
	label   string
	lastAt  time.Time
	started bool
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.read += int64(n)
	now := time.Now()
	if !p.started {
		p.started = true
		p.lastAt = now
		p.print()
	} else if now.Sub(p.lastAt) >= 100*time.Millisecond {
		p.lastAt = now
		p.print()
	}
	return n, err
}

func (p *progressReader) print() {
	mib := func(b int64) float64 { return float64(b) / (1 << 20) }
	if p.total > 0 {
		pct := float64(p.read) * 100 / float64(p.total)
		bar := renderBar(pct, 30)
		fmt.Fprintf(os.Stderr, "\r%s %s %5.1f%%  %.1f / %.1f MiB",
			p.label, bar, pct, mib(p.read), mib(p.total))
	} else {
		fmt.Fprintf(os.Stderr, "\r%s %.1f MiB", p.label, mib(p.read))
	}
}

func (p *progressReader) done() {
	if !p.started {
		return
	}
	p.print()
	fmt.Fprintln(os.Stderr)
}

// renderBar returns a Unicode progress bar of the given width filled
// proportionally to pct (0..100). Uses block glyphs that align in
// monospace fonts on every terminal we ship to.
func renderBar(pct float64, width int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := int(pct * float64(width) / 100)
	if filled > width {
		filled = width
	}
	bar := make([]byte, 0, width+2)
	bar = append(bar, '[')
	for i := 0; i < width; i++ {
		if i < filled {
			bar = append(bar, '#')
		} else {
			bar = append(bar, '.')
		}
	}
	bar = append(bar, ']')
	return string(bar)
}

// extractMutagenFromTarGz walks the tarball once and returns both the
// mutagen binary and the agent bundle as raw bytes. Single pass keeps
// the entire archive in memory only briefly.
func extractMutagenFromTarGz(blob []byte, binName, bundleName string) (bin, bundle []byte, err error) {
	gz, err := gzip.NewReader(bytes.NewReader(blob))
	if err != nil {
		return nil, nil, fmt.Errorf("gzip open: %w", err)
	}
	defer func() { _ = gz.Close() }() //nolint:errcheck
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return bin, bundle, nil
		}
		if err != nil {
			return nil, nil, fmt.Errorf("tar read: %w", err)
		}
		name := filepath.Base(h.Name)
		switch name {
		case binName:
			bin, err = io.ReadAll(tr)
			if err != nil {
				return nil, nil, fmt.Errorf("read %s: %w", name, err)
			}
		case bundleName:
			bundle, err = io.ReadAll(tr)
			if err != nil {
				return nil, nil, fmt.Errorf("read %s: %w", name, err)
			}
		}
		if bin != nil && bundle != nil {
			return bin, bundle, nil
		}
	}
}

func extractMutagenFromZip(blob []byte, binName, bundleName string) (bin, bundle []byte, err error) {
	zr, err := zip.NewReader(bytes.NewReader(blob), int64(len(blob)))
	if err != nil {
		return nil, nil, fmt.Errorf("zip open: %w", err)
	}
	for _, f := range zr.File {
		name := filepath.Base(f.Name)
		var dst *[]byte
		switch name {
		case binName:
			dst = &bin
		case bundleName:
			dst = &bundle
		default:
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, nil, fmt.Errorf("zip entry open: %w", err)
		}
		buf, err := io.ReadAll(rc)
		_ = rc.Close() //nolint:errcheck
		if err != nil {
			return nil, nil, fmt.Errorf("read %s: %w", name, err)
		}
		*dst = buf
		if bin != nil && bundle != nil {
			break
		}
	}
	return bin, bundle, nil
}
