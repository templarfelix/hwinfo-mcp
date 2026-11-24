// Command marketplace points the repository at a published release, so that
// nobody has to copy a version and a checksum by hand:
//
//	go run ./tools/marketplace -version v1.2.3
//
// It does two things, each only after checking the release's own files.
//
// It downloads the release's SHA256SUMS and its hwinfo-plugin.zip, checks one
// against the other, and rewrites the plugin's archive URL and SHA-256 in
// .claude-plugin/marketplace.json. Plugin users get the new version from the
// commit of that change.
//
// And it points mcpb/hwinfo.json, the pin of HWiNFO's portable package, at
// the release's bundle that carries the package, after checking that the
// bundle holds exactly the pinned file. The mirror the package is downloaded
// from keeps only HWiNFO's current version; from then on tools/release can
// take the package out of that bundle instead.
//
// It therefore works only once the release is public. Run it from the
// repository root and commit the changes.
package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

	// bundleName is the bundle that carries HWiNFO's package, and
	// packageDir the folder of the bundle the package sits in.
	bundleName = "hwinfo-mcp-with-hwinfo.mcpb"
	packageDir = "hwinfo"
	// maxBundle bounds what is read from the network for that bundle.
	maxBundle = 256 << 20
)

var (
	versionRE = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)

	// The two values of the plugin's source that change with a release. Each
	// must occur exactly once in the marketplace file.
	urlRE = regexp.MustCompile(`("url":\s*")[^"]*/` + regexp.QuoteMeta(archiveName) + `(")`)
	shaRE = regexp.MustCompile(`("sha256":\s*")[0-9a-fA-F]{64}(")`)

	// The value of the pin that changes with a release. It must occur
	// exactly once in the pin file.
	bundleRE = regexp.MustCompile(`("bundle":\s*")[^"]*(")`)
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("marketplace: ")

	version := flag.String("version", "", "published release to point at, as a tag: v1.2.3")
	file := flag.String("file", ".claude-plugin/marketplace.json", "marketplace file to rewrite")
	pin := flag.String("pin", "mcpb/hwinfo.json", "pin of HWiNFO's package to rewrite")
	base := flag.String("base", "https://github.com/templarfelix/hwinfo-mcp/releases/download", "where releases are downloaded from")
	flag.Parse()

	if !versionRE.MatchString(*version) {
		log.Fatalf("-version %q is not of the form v1.2.3", *version)
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	from := strings.TrimRight(*base, "/")

	archiveURL, digest, err := repoint(client, *file, from, *version)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s now points at\n  %s\n  sha256 %s\n", *file, archiveURL, digest)

	bundleURL, err := keepPackage(client, *pin, from, *version)
	if err != nil {
		log.Fatalf("%s was updated, %s was not: %v", *file, *pin, err)
	}
	fmt.Printf("%s now names the bundle that carries the package\n  %s\n", *pin, bundleURL)
}

// repoint rewrites file to name the plugin archive of the given release, and
// returns the URL and digest it wrote. The file is left as it was unless the
// release's SHA256SUMS lists the archive and the downloaded archive matches.
func repoint(client *http.Client, file, base, version string) (archiveURL, digest string, err error) {
	release := base + "/" + version
	archiveURL = release + "/" + archiveName

	if _, digest, err = listedFile(client, release, archiveName, maxArchive); err != nil {
		return "", "", err
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

// keepPackage points the pin of HWiNFO's package at the given release's
// bundle that carries it, and returns the URL it wrote. The pin is left as it
// was unless the release's SHA256SUMS lists that bundle, the downloaded bundle
// matches, and the bundle holds exactly the file the pin describes.
func keepPackage(client *http.Client, file, base, version string) (bundleURL string, err error) {
	release := base + "/" + version
	bundleURL = release + "/" + bundleName

	data, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	var pin struct {
		File   string `json:"file"`
		Size   int64  `json:"size"`
		SHA256 string `json:"sha256"`
	}
	if err := json.Unmarshal(data, &pin); err != nil {
		return "", fmt.Errorf("%s: %w", file, err)
	}
	if n := len(bundleRE.FindAllIndex(data, -1)); n != 1 {
		return "", fmt.Errorf("%s: the bundle occurs %d times, expected 1", file, n)
	}

	bundle, _, err := listedFile(client, release, bundleName, maxBundle)
	if err != nil {
		return "", err
	}
	pkg, err := entry(bundle, packageDir+"/"+pin.File)
	if err != nil {
		return "", fmt.Errorf("%s: %w", bundleURL, err)
	}
	if sum := sha256.Sum256(pkg); int64(len(pkg)) != pin.Size || hex.EncodeToString(sum[:]) != pin.SHA256 {
		return "", fmt.Errorf("%s carries a %s of %d bytes hashing to %s, but %s pins %d bytes and %s",
			bundleURL, pin.File, len(pkg), hex.EncodeToString(sum[:]), file, pin.Size, pin.SHA256)
	}

	updated := bundleRE.ReplaceAll(data, []byte("${1}"+bundleURL+"${2}"))
	if err := os.WriteFile(file, updated, 0o644); err != nil {
		return "", err
	}
	return bundleURL, nil
}

// listedFile downloads the named file of a release and the release's
// SHA256SUMS, and returns the file and its digest once one matches the other.
func listedFile(client *http.Client, release, name string, limit int64) (data []byte, digest string, err error) {
	sums, err := fetch(client, release+"/SHA256SUMS", 1<<20)
	if err != nil {
		return nil, "", err
	}
	digest, err = listed(sums, name)
	if err != nil {
		return nil, "", fmt.Errorf("%s/SHA256SUMS: %w", release, err)
	}
	data, err = fetch(client, release+"/"+name, limit)
	if err != nil {
		return nil, "", err
	}
	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != digest {
		return nil, "", fmt.Errorf("%s/%s hashes to %s, but SHA256SUMS lists %s", release, name, hex.EncodeToString(sum[:]), digest)
	}
	return data, digest, nil
}

// entry returns the content of the entry called name in a zip archive.
func entry(archive []byte, name string) ([]byte, error) {
	r, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, err
	}
	for _, f := range r.File {
		if f.Name != name {
			continue
		}
		src, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer src.Close()
		data, err := io.ReadAll(io.LimitReader(src, maxBundle+1))
		if err != nil {
			return nil, err
		}
		if len(data) > maxBundle {
			return nil, fmt.Errorf("%s is larger than the %d bytes expected", name, maxBundle)
		}
		return data, nil
	}
	return nil, fmt.Errorf("no %s inside", name)
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
