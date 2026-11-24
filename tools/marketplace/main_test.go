package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sample = `{
  "name": "hwinfo-mcp",
  "plugins": [
    {
      "name": "hwinfo",
      "homepage": "https://github.com/templarfelix/hwinfo-mcp",
      "source": {
        "source": "archive",
        "url": "https://github.com/templarfelix/hwinfo-mcp/releases/download/v0.2.2/hwinfo-plugin.zip",
        "sha256": "ad931d6fec8b5ab1c91e61b8f2d510eb618c2aae3563c44de8884410e9839b30"
      }
    }
  ]
}
`

func digestOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// release serves one release the way GitHub does: its files under
// /<version>/. sums is the SHA256SUMS it publishes.
func release(t *testing.T, version string, archive []byte, sums string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/"+version+"/SHA256SUMS", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, sums) })
	mux.HandleFunc("/"+version+"/"+archiveName, func(w http.ResponseWriter, _ *http.Request) { w.Write(archive) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func marketplaceFile(t *testing.T) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "marketplace.json")
	if err := os.WriteFile(file, []byte(sample), 0o644); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestRepoint(t *testing.T) {
	archive := []byte("the plugin archive of v1.2.3")
	sums := "0000000000000000000000000000000000000000000000000000000000000000  hwinfo-mcp.mcpb\n" +
		digestOf(archive) + "  " + archiveName + "\n"
	srv := release(t, "v1.2.3", archive, sums)
	file := marketplaceFile(t)

	archiveURL, digest, err := repoint(srv.Client(), file, srv.URL, "v1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if want := srv.URL + "/v1.2.3/" + archiveName; archiveURL != want || digest != digestOf(archive) {
		t.Errorf("repoint returned %s and %s, want %s and %s", archiveURL, digest, want, digestOf(archive))
	}

	got, _ := os.ReadFile(file)
	// Only the two values changed; the rest of the file is byte for byte the same.
	want := strings.NewReplacer(
		"https://github.com/templarfelix/hwinfo-mcp/releases/download/v0.2.2/hwinfo-plugin.zip", archiveURL,
		"ad931d6fec8b5ab1c91e61b8f2d510eb618c2aae3563c44de8884410e9839b30", digest,
	).Replace(sample)
	if string(got) != want {
		t.Errorf("marketplace file:\n%s\nwant:\n%s", got, want)
	}
	var parsed any
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Errorf("the rewritten file is not JSON: %v", err)
	}
}

// Anything that does not add up leaves the file exactly as it was.
func TestRepointRefusesWhatDoesNotAddUp(t *testing.T) {
	archive := []byte("the plugin archive")
	good := digestOf(archive) + "  " + archiveName + "\n"

	tests := map[string]struct {
		version string // release asked for; the server only has v1.2.3
		archive []byte
		sums    string
		wantErr string
	}{
		"archive differs from its checksum": {"v1.2.3", []byte("tampered"), good, "but SHA256SUMS lists"},
		"checksums do not list the archive": {"v1.2.3", archive, digestOf(archive) + "  something-else.zip\n", "no entry for"},
		"entry is not a SHA-256":            {"v1.2.3", archive, "abc123  " + archiveName + "\n", "not a SHA-256"},
		"release does not exist":            {"v9.9.9", archive, good, "404"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			srv := release(t, "v1.2.3", tt.archive, tt.sums)
			file := marketplaceFile(t)

			_, _, err := repoint(srv.Client(), file, srv.URL, tt.version)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %v, want one mentioning %q", err, tt.wantErr)
			}
			if got, _ := os.ReadFile(file); string(got) != sample {
				t.Errorf("the file was changed although the release did not add up:\n%s", got)
			}
		})
	}
}

func TestRewriteRefusesAFileOfAnotherShape(t *testing.T) {
	const url, digest = "https://example.com/v1.2.3/hwinfo-plugin.zip", "1111111111111111111111111111111111111111111111111111111111111111"
	for name, data := range map[string]string{
		"no archive source": `{"plugins": [{"name": "hwinfo", "source": "./plugin"}]}`,
		"two plugins":       sample + sample,
		"no checksum":       strings.Replace(sample, `"sha256"`, `"sha512"`, 1),
	} {
		if _, err := rewrite([]byte(data), url, digest); err == nil {
			t.Errorf("%s: rewrite accepted the file", name)
		}
	}
}

// The marketplace file in the repository must have the shape this tool
// rewrites, and must name a release of this project: a hand edit that breaks
// either would leave the next release without a way to update it.
func TestRepositoryMarketplaceFile(t *testing.T) {
	data, err := os.ReadFile(filepath.FromSlash("../../.claude-plugin/marketplace.json"))
	if err != nil {
		t.Fatal(err)
	}
	const digest = "2222222222222222222222222222222222222222222222222222222222222222"
	updated, err := rewrite(data, "https://example.com/v9.9.9/"+archiveName, digest)
	if err != nil {
		t.Fatalf(".claude-plugin/marketplace.json: %v", err)
	}
	if !bytes.Contains(updated, []byte(digest)) {
		t.Error("the rewrite did not set the checksum")
	}

	var market struct {
		Plugins []struct {
			Name   string `json:"name"`
			Source struct {
				Source string `json:"source"`
				URL    string `json:"url"`
				SHA256 string `json:"sha256"`
			} `json:"source"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(data, &market); err != nil {
		t.Fatal(err)
	}
	if len(market.Plugins) != 1 {
		t.Fatalf("%d plugins listed, want 1", len(market.Plugins))
	}
	src := market.Plugins[0].Source
	const prefix = "https://github.com/templarfelix/hwinfo-mcp/releases/download/"
	version, ok := strings.CutSuffix(strings.TrimPrefix(src.URL, prefix), "/"+archiveName)
	if src.Source != "archive" || !strings.HasPrefix(src.URL, prefix) || !ok || !versionRE.MatchString(version) {
		t.Errorf("source = %+v, want the archive of a tagged release under %s", src, prefix)
	}
	if strings.Contains(version, "-") {
		t.Errorf("the marketplace points at the pre-release %s", version)
	}
	if _, err := hex.DecodeString(src.SHA256); err != nil || len(src.SHA256) != 64 {
		t.Errorf("sha256 = %q, want 64 hex digits", src.SHA256)
	}
}
