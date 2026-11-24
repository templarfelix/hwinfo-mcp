// Command marketplace points .claude-plugin/marketplace.json at a published
// release, so that nobody has to copy a version and a checksum by hand:
//
//	go run ./tools/marketplace -version v1.2.3
//
// It downloads the release's SHA256SUMS and its hwinfo-plugin.zip, checks one
// against the other, and only then rewrites the plugin's archive URL and
// SHA-256 in the marketplace file. It therefore works only once the release
// is public. Run it from the repository root and commit the change: plugin
// users get the new version from that commit.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

const (
	archiveName = "hwinfo-plugin.zip"
	// maxArchive bounds what is read from the network. The plugin archive is
	// a few megabytes.
	maxArchive = 64 << 20
)

var (
	versionRE = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)

	// The two values of the plugin's source that change with a release. Each
	// must occur exactly once in the marketplace file.
	urlRE = regexp.MustCompile(`("url":\s*")[^"]*/` + regexp.QuoteMeta(archiveName) + `(")`)
	shaRE = regexp.MustCompile(`("sha256":\s*")[0-9a-fA-F]{64}(")`)
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("marketplace: ")

	version := flag.String("version", "", "published release to point at, as a tag: v1.2.3")
	file := flag.String("file", ".claude-plugin/marketplace.json", "marketplace file to rewrite")
	base := flag.String("base", "https://github.com/templarfelix/hwinfo-mcp/releases/download", "where releases are downloaded from")
	flag.Parse()

	if !versionRE.MatchString(*version) {
		log.Fatalf("-version %q is not of the form v1.2.3", *version)
	}
	client := &http.Client{Timeout: 2 * time.Minute}
	archiveURL, digest, err := repoint(client, *file, strings.TrimRight(*base, "/"), *version)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s now points at\n  %s\n  sha256 %s\n", *file, archiveURL, digest)
}

// repoint rewrites file to name the plugin archive of the given release, and
// returns the URL and digest it wrote. The file is left as it was unless the
// release's SHA256SUMS lists the archive and the downloaded archive matches.
func repoint(client *http.Client, file, base, version string) (archiveURL, digest string, err error) {
	release := base + "/" + version
	archiveURL = release + "/" + archiveName

	sums, err := fetch(client, release+"/SHA256SUMS", 1<<20)
	if err != nil {
		return "", "", err
	}
	digest, err = listed(sums, archiveName)
	if err != nil {
		return "", "", fmt.Errorf("%s/SHA256SUMS: %w", release, err)
	}
	archive, err := fetch(client, archiveURL, maxArchive)
	if err != nil {
		return "", "", err
	}
	if sum := sha256.Sum256(archive); hex.EncodeToString(sum[:]) != digest {
		return "", "", fmt.Errorf("%s hashes to %s, but SHA256SUMS lists %s", archiveURL, hex.EncodeToString(sum[:]), digest)
	}

	data, err := os.ReadFile(file)
	if err != nil {
		return "", "", err
	}
	updated, err := rewrite(data, archiveURL, digest)
	if err != nil {
		return "", "", fmt.Errorf("%s: %w", file, err)
	}
	if err := os.WriteFile(file, updated, 0o644); err != nil {
		return "", "", err
	}
	return archiveURL, digest, nil
}

// rewrite returns the marketplace file with the archive URL and digest
// replaced. It refuses a file in which either value does not occur exactly
// once: that file has changed shape, and guessing would corrupt it.
func rewrite(data []byte, archiveURL, digest string) ([]byte, error) {
	for name, re := range map[string]*regexp.Regexp{"archive URL": urlRE, "sha256": shaRE} {
		if n := len(re.FindAllIndex(data, -1)); n != 1 {
			return nil, fmt.Errorf("the %s occurs %d times, expected 1", name, n)
		}
	}
	data = urlRE.ReplaceAll(data, []byte("${1}"+archiveURL+"${2}"))
	data = shaRE.ReplaceAll(data, []byte("${1}"+digest+"${2}"))
	return data, nil
}

// listed returns the digest that a SHA256SUMS file gives for name.
func listed(sums []byte, name string) (string, error) {
	for _, line := range bytes.Split(sums, []byte("\n")) {
		fields := strings.Fields(string(line))
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			digest := strings.ToLower(fields[0])
			if len(digest) != 64 {
				return "", fmt.Errorf("the entry for %s is not a SHA-256", name)
			}
			if _, err := hex.DecodeString(digest); err != nil {
				return "", fmt.Errorf("the entry for %s is not a SHA-256", name)
			}
			return digest, nil
		}
	}
	return "", fmt.Errorf("no entry for %s", name)
}

// fetch downloads url and fails on anything but a complete 200 response of
// at most limit bytes.
func fetch(client *http.Client, url string, limit int64) ([]byte, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", url, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s: larger than the %d bytes expected", url, limit)
	}
	return data, nil
}
