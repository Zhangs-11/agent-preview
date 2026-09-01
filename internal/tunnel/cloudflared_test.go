package tunnel

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"runtime"
	"testing"
)

func TestVerifyDigest(t *testing.T) {
	data := []byte("cloudflared")
	digest := sha256.Sum256(data)
	if err := verifyDigest(data, "sha256:"+hex.EncodeToString(digest[:])); err != nil {
		t.Fatal(err)
	}
	if err := verifyDigest([]byte("tampered"), "sha256:"+hex.EncodeToString(digest[:])); err == nil {
		t.Fatal("tampered download passed digest verification")
	}
}

func TestExtractBinary(t *testing.T) {
	var archive bytes.Buffer
	gzipWriter := gzip.NewWriter(&archive)
	tarWriter := tar.NewWriter(gzipWriter)
	content := []byte("binary-content")
	if err := tarWriter.WriteHeader(&tar.Header{Name: "cloudflared", Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}

	extracted, err := extractBinary(archive.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(extracted, content) {
		t.Fatalf("extracted = %q", extracted)
	}
}

func TestFindBinaryRejectsInvalidConfiguredPath(t *testing.T) {
	t.Setenv("AGENT_PREVIEW_CLOUDFLARED", "/definitely/missing/cloudflared")
	if _, err := FindBinary(); err == nil {
		t.Fatal("invalid AGENT_PREVIEW_CLOUDFLARED path was accepted")
	}
}

func TestSelectAssetRequiresDigest(t *testing.T) {
	var name string
	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/arm64":
		name = "cloudflared-darwin-arm64.tgz"
	case "darwin/amd64":
		name = "cloudflared-darwin-amd64.tgz"
	case "linux/arm64":
		name = "cloudflared-linux-arm64"
	case "linux/amd64":
		name = "cloudflared-linux-amd64"
	default:
		t.Skip("automatic setup is not supported on this platform")
	}
	if _, err := selectAsset([]asset{{Name: name}}); err == nil {
		t.Fatal("release asset without digest was accepted")
	}
	selected, err := selectAsset([]asset{{Name: name, Digest: "sha256:abc", BrowserDownloadURL: "https://example.test"}})
	if err != nil {
		t.Fatal(err)
	}
	if selected.Name != name {
		t.Fatalf("selected asset = %q", selected.Name)
	}
}
