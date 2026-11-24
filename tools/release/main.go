// Command release builds everything a release publishes into one directory:
//
//	hwinfo-mcp-windows-amd64.exe   the server
//	hwinfo-mcp-windows-arm64.exe
//	hwinfo-plugin.zip              the Claude Code plugin: skills, MCP
//	                               configuration and the amd64 server
//	hwinfo-mcp.mcpb                the MCP bundle for Claude Desktop and the
//	                               MCP Registry
//	hwinfo-mcp-with-hwinfo.mcpb    the same bundle with HWiNFO's portable
//	                               package, a settings file and a setup helper
//	server.json                    the MCP Registry entry for hwinfo-mcp.mcpb
//	SHA256SUMS
//
// CI runs it on a tag. Running it by hand produces the same files, which is
// how the plugin and the bundles are tested before a release:
//
//	go run ./tools/release -version v1.2.3 -out dist
//
// It must be started from the repository root. The bundles are written here,
// as zip archives like the plugin, and then checked with the official mcpb
// tool through npx, which needs Node.js; pass -mcpb "" to skip that check. The
// job that publishes a release skips it, so that no code from npm runs where
// the release files are made: the same manifest was checked by the tests'
// trial package on the way there.
//
// HWiNFO's portable package is not in the repository. mcpb/hwinfo.json names
// it and pins its size and SHA-256; this tool downloads it once into the
// directory given with -hwinfo and refuses any file that is not the pinned
// one. The package is copied into the bundle as it is and is never run here.
package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	// devVersion is the placeholder the manifests in the repository carry.
	devVersion = "0.0.0-dev"
	// devSHA is the placeholder for the bundle's digest in mcpb/server.json.
	devSHA = "0000000000000000000000000000000000000000000000000000000000000000"

	serverInArchive = "server/hwinfo-mcp.exe"
	// bundleIcon is the icon mcpb/manifest.json names. It travels inside the
	// bundle, next to the manifest.
	bundleIcon = "icon.png"

	bundleName = "hwinfo-mcp.mcpb"
	// fullBundleName is the bundle that also carries HWiNFO.
	fullBundleName = "hwinfo-mcp-with-hwinfo.mcpb"
	// hwinfoDir is the folder of that bundle in which HWiNFO's package, the
	// settings file and the setup helper sit. The sources of the latter two
	// are in the folder of the same name under mcpb/.
	hwinfoDir = "hwinfo"
	// noteAnchor is how the long description in mcpb/manifest.json ends. The
	// bundle that carries HWiNFO gets a paragraph about it after this.
	noteAnchor = `This project is not affiliated with HWiNFO or its authors."`

	// maxBundle bounds what is read from the network when the package has to
	// be taken out of a published bundle.
	maxBundle = 256 << 20
)

// hwinfoFiles are this project's own files in the hwinfo folder. Windows
// reads them as they are, so they are packed with CRLF line endings whatever
// the checkout has: cmd.exe can lose its place in a batch file that has only
// line feeds.
var hwinfoFiles = []string{"HWiNFO64.INI", "README.txt", "Setup-HWiNFO.cmd"}

var (
	versionRE = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)
	// The setup helper finds the package next to it by this pattern.
	packageFileRE = regexp.MustCompile(`^hwi_[0-9A-Za-z_]+\.zip$`)
	sha256RE      = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("release: ")

	cache := "."
	if dir, err := os.UserCacheDir(); err == nil {
		cache = dir
	}
	version := flag.String("version", "", "version to build, as a tag: v1.2.3")
	out := flag.String("out", "dist", "directory to write the release files to")
	mcpb := flag.String("mcpb", "npx --yes @anthropic-ai/mcpb@2.1.2", `command that runs the official mcpb tool to check the bundles; "" skips the check`)
	hwinfo := flag.String("hwinfo", filepath.Join(cache, "hwinfo-mcp-release"), "directory in which HWiNFO's portable package is kept between runs")
	flag.Parse()

	if !versionRE.MatchString(*version) {
		log.Fatalf("-version %q is not of the form v1.2.3", *version)
	}
	if _, err := os.Stat("go.mod"); err != nil {
		log.Fatal("run this from the repository root")
	}
	if err := run(*version, *out, strings.Fields(*mcpb), *hwinfo); err != nil {
		log.Fatal(err)
	}
}

func run(version, out string, mcpb []string, hwinfoCache string) error {
	plain := strings.TrimPrefix(version, "v")
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	out, err := filepath.Abs(out)
	if err != nil {
		return err
	}

	// The package comes first: without it there is no release, and finding
	// that out should not take two builds.
	pin, err := readPin(filepath.Join("mcpb", "hwinfo.json"))
	if err != nil {
		return err
	}
	pkg, err := obtain(&http.Client{Timeout: 10 * time.Minute}, pin, hwinfoCache)
	if err != nil {
		return err
	}

	// The server, for both architectures Windows runs on.
	artifacts := []string{}
	exe := map[string]string{}
	for _, arch := range []string{"amd64", "arm64"} {
		name := "hwinfo-mcp-windows-" + arch + ".exe"
		exe[arch] = filepath.Join(out, name)
		build := exec.Command("go", "build", "-trimpath",
			"-ldflags", "-s -w -X main.version="+version,
			"-o", exe[arch], ".")
		build.Env = append(os.Environ(), "GOOS=windows", "GOARCH="+arch, "CGO_ENABLED=0")
		if err := runCommand(build); err != nil {
			return fmt.Errorf("building %s: %w", name, err)
		}
		artifacts = append(artifacts, name)
	}

	stage, err := os.MkdirTemp("", "hwinfo-release-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)

	// The plugin: what is in plugin/, plus the server, with the version set.
	pluginDir := filepath.Join(stage, "plugin")
	if err := copyTree("plugin", pluginDir); err != nil {
		return err
	}
	if err := copyFile(exe["amd64"], filepath.Join(pluginDir, filepath.FromSlash(serverInArchive))); err != nil {
		return err
	}
	if err := stamp(filepath.Join(pluginDir, ".claude-plugin", "plugin.json"), devVersion, plain, 1); err != nil {
		return err
	}
	if err := zipDir(pluginDir, filepath.Join(out, "hwinfo-plugin.zip"), sourceTime()); err != nil {
		return err
	}
	artifacts = append(artifacts, "hwinfo-plugin.zip")

	// The MCP bundle: a zip archive with the manifest at its top level, the
	// icon next to it and the server where the manifest says it is.
	bundleDir := filepath.Join(stage, "bundle")
	if err := stageBundle(bundleDir, "mcpb", plain, exe["amd64"]); err != nil {
		return err
	}
	bundle := filepath.Join(out, bundleName)
	if err := pack(bundleDir, bundle, mcpb); err != nil {
		return err
	}
	artifacts = append(artifacts, bundleName)

	// The same bundle with HWiNFO next to the server.
	fullDir := filepath.Join(stage, "bundle-with-hwinfo")
	if err := stageBundle(fullDir, "mcpb", plain, exe["amd64"]); err != nil {
		return err
	}
	if err := addHWiNFO(fullDir, filepath.Join("mcpb", hwinfoDir), pin, pkg); err != nil {
		return err
	}
	if err := pack(fullDir, filepath.Join(out, fullBundleName), mcpb); err != nil {
		return err
	}
	artifacts = append(artifacts, fullBundleName)

	// The MCP Registry entry, which is for the bundle without HWiNFO.
	digest, err := sha256File(bundle)
	if err != nil {
		return err
	}
	entry := filepath.Join(out, "server.json")
	if err := copyFile(filepath.Join("mcpb", "server.json"), entry); err != nil {
		return err
	}
	// The version appears twice: on its own and inside the download URL.
	if err := stamp(entry, devVersion, plain, 2); err != nil {
		return err
	}
	if err := stamp(entry, devSHA, digest, 1); err != nil {
		return err
	}

	sort.Strings(artifacts)
	var sums bytes.Buffer
	for _, name := range artifacts {
		digest, err := sha256File(filepath.Join(out, name))
		if err != nil {
			return err
		}
		fmt.Fprintf(&sums, "%s  %s\n", digest, name)
	}
	if err := os.WriteFile(filepath.Join(out, "SHA256SUMS"), sums.Bytes(), 0o644); err != nil {
		return err
	}

	fmt.Printf("%s written to %s:\n%s", version, out, sums.String())
	return nil
}

func runCommand(cmd *exec.Cmd) error {
	// Everything a tool prints is progress, so it goes to stderr and leaves
	// stdout to this program's own summary.
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	return cmd.Run()
}

// stageBundle lays out in dir what both bundles consist of: the manifest and
// the icon from sources, with the version set, and the server.
func stageBundle(dir, sources, plain, server string) error {
	for _, name := range []string{"manifest.json", bundleIcon} {
		if err := copyFile(filepath.Join(sources, name), filepath.Join(dir, name)); err != nil {
			return err
		}
	}
	if err := stamp(filepath.Join(dir, "manifest.json"), devVersion, plain, 1); err != nil {
		return err
	}
	return copyFile(server, filepath.Join(dir, filepath.FromSlash(serverInArchive)))
}

// pack archives a staged bundle and lets the official tool, when there is
// one, have the last word on the manifest and on whether it can read the
// archive back.
func pack(dir, bundle string, mcpb []string) error {
	if err := zipDir(dir, bundle, sourceTime()); err != nil {
		return err
	}
	if len(mcpb) == 0 {
		return nil
	}
	for _, args := range [][]string{{"validate", dir}, {"info", bundle}} {
		tool := exec.Command(mcpb[0], append(mcpb[1:len(mcpb):len(mcpb)], args...)...)
		if err := runCommand(tool); err != nil {
			return fmt.Errorf("mcpb %s %s: %w", args[0], filepath.Base(bundle), err)
		}
	}
	return nil
}

// hwinfoPackage is mcpb/hwinfo.json: the HWiNFO portable package that the
// bundle "with HWiNFO" carries. Size and SHA-256 pin it, so that a release
// can only contain the file that was looked at when the pin was written.
type hwinfoPackage struct {
	// Version is HWiNFO's version, for the texts that name it.
	Version string `json:"version"`
	// File is the package's file name, as its author publishes it.
	File   string `json:"file"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	// URL is where the package is downloaded from. The mirror in use keeps
	// only HWiNFO's current version, so it stops serving the file one day.
	URL string `json:"url"`
	// Bundle is a published bundle of this project that carries the same
	// file. The package is taken out of it when URL no longer serves it.
	// It is empty until a release has carried the file.
	Bundle string `json:"bundle"`
}

func readPin(file string) (hwinfoPackage, error) {
	var pin hwinfoPackage
	data, err := os.ReadFile(file)
	if err != nil {
		return pin, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&pin); err != nil {
		return pin, fmt.Errorf("%s: %w", file, err)
	}
	if err := pin.check(); err != nil {
		return pin, fmt.Errorf("%s: %w", file, err)
	}
	return pin, nil
}

// check reports what is wrong with the pin itself.
func (p hwinfoPackage) check() error {
	switch {
	case p.Version == "":
		return errors.New("no version")
	case !packageFileRE.MatchString(p.File):
		return fmt.Errorf("file %q is not named like HWiNFO's portable package, hwi_NNN.zip", p.File)
	case p.Size <= 0:
		return errors.New("no size")
	case !sha256RE.MatchString(p.SHA256):
		return errors.New("sha256 is not 64 lower-case hexadecimal digits")
	case !strings.HasPrefix(p.URL, "https://"):
		return fmt.Errorf("url %q is not an https address", p.URL)
	case p.Bundle != "" && !strings.HasPrefix(p.Bundle, "https://"):
		return fmt.Errorf("bundle %q is not an https address", p.Bundle)
	}
	return nil
}

// verify reports whether file is the pinned package.
func (p hwinfoPackage) verify(file string) error {
	info, err := os.Stat(file)
	if err != nil {
		return err
	}
	if info.Size() != p.Size {
		return fmt.Errorf("%s is %d bytes, but %d are pinned", file, info.Size(), p.Size)
	}
	digest, err := sha256File(file)
	if err != nil {
		return err
	}
	if digest != p.SHA256 {
		return fmt.Errorf("%s hashes to %s, but %s is pinned", file, digest, p.SHA256)
	}
	return nil
}

// obtain returns the path of the pinned package in dir. A file that is
// already there is used if it is the pinned one. Otherwise the package is
// downloaded from the pin's URL or, failing that, taken out of the published
// bundle the pin names. Whatever arrives is kept only if it is the pinned
// file.
func obtain(client *http.Client, pin hwinfoPackage, dir string) (string, error) {
	file := filepath.Join(dir, pin.File)
	if pin.verify(file) == nil {
		return file, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	part := file + ".part"
	defer os.Remove(part)

	keep := func() error {
		if err := pin.verify(part); err != nil {
			return err
		}
		return os.Rename(part, file)
	}

	fmt.Fprintf(os.Stderr, "release: downloading %s\n", pin.URL)
	err := download(client, pin.URL, part, pin.Size)
	if err == nil {
		err = keep()
	}
	if err == nil {
		return file, nil
	}
	if pin.Bundle == "" {
		return "", fmt.Errorf("HWiNFO's package: %w. No published bundle carries it yet, so there is nowhere else to take it from; if HWiNFO has a newer version, point mcpb/hwinfo.json at it", err)
	}

	fmt.Fprintf(os.Stderr, "release: %v\nrelease: taking the package out of %s\n", err, pin.Bundle)
	from := filepath.Join(dir, "bundle.part")
	defer os.Remove(from)
	other := download(client, pin.Bundle, from, maxBundle)
	if other == nil {
		other = extract(from, hwinfoDir+"/"+pin.File, part)
	}
	if other == nil {
		other = keep()
	}
	if other != nil {
		return "", fmt.Errorf("HWiNFO's package: %w; and from the published bundle: %w. If HWiNFO has a newer version, point mcpb/hwinfo.json at it", err, other)
	}
	return file, nil
}

// download saves url as file and fails on anything but a complete 200
// response of at most limit bytes.
func download(client *http.Client, url, file string, limit int64) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", url, resp.Status)
	}
	dst, err := os.Create(file)
	if err != nil {
		return err
	}
	n, err := io.Copy(dst, io.LimitReader(resp.Body, limit+1))
	if err := errors.Join(err, dst.Close()); err != nil {
		return fmt.Errorf("%s: %w", url, err)
	}
	if n > limit {
		return fmt.Errorf("%s: larger than the %d bytes expected", url, limit)
	}
	return nil
}

// extract copies the entry called name out of a zip archive.
func extract(archive, name, to string) error {
	r, err := zip.OpenReader(archive)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		if f.Name != name {
			continue
		}
		src, err := f.Open()
		if err != nil {
			return err
		}
		defer src.Close()
		dst, err := os.Create(to)
		if err != nil {
			return err
		}
		_, err = io.Copy(dst, src)
		return errors.Join(err, dst.Close())
	}
	return fmt.Errorf("%s holds no %s", archive, name)
}

// addHWiNFO turns a staged bundle into the one that carries HWiNFO. It adds
// the hwinfo folder: this project's own files from sources, and the package
// exactly as it was obtained. And it says so in the manifest, because whoever
// installs the bundle should learn from it what else is inside.
func addHWiNFO(bundleDir, sources string, pin hwinfoPackage, pkg string) error {
	dir := filepath.Join(bundleDir, hwinfoDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, name := range hwinfoFiles {
		data, err := os.ReadFile(filepath.Join(sources, name))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, name), crlf(data), 0o644); err != nil {
			return err
		}
	}
	// The notice names the package it stands next to.
	notice := filepath.Join(dir, "README.txt")
	for _, s := range []struct {
		placeholder, value string
		want               int
	}{
		{"HWINFO_PACKAGE_FILE", pin.File, 2},
		{"HWINFO_PACKAGE_VERSION", pin.Version, 1},
		{"HWINFO_PACKAGE_SHA256", pin.SHA256, 1},
	} {
		if err := stamp(notice, s.placeholder, s.value, s.want); err != nil {
			return err
		}
	}
	if err := copyFile(pkg, filepath.Join(dir, pin.File)); err != nil {
		return err
	}
	return stamp(filepath.Join(bundleDir, "manifest.json"), noteAnchor,
		strings.TrimSuffix(noteAnchor, `"`)+`\n\n`+bundleNote(pin)+`"`, 1)
}

// bundleNote is the paragraph the manifest of the bundle that carries HWiNFO
// adds to its long description, escaped for a JSON string. It keeps apart
// what is HWiNFO's and what is this project's.
func bundleNote(pin hwinfoPackage) string {
	note := fmt.Sprintf("This package also carries HWiNFO's portable package, version %s, exactly as its author publishes it, in its %s folder. HWiNFO is not part of this project: it belongs to REALiX, is freeware for non-commercial use under its own licence, and is included with its author's permission. The settings file and the setup helper next to it are this project's own. Nothing in that folder runs unless you start it.", pin.Version, hwinfoDir)
	quoted, err := json.Marshal(note)
	if err != nil {
		panic(err) // a string always marshals
	}
	return string(quoted[1 : len(quoted)-1])
}

// crlf returns text with every line ending as CR LF, whichever it had.
func crlf(text []byte) []byte {
	text = bytes.ReplaceAll(text, []byte("\r\n"), []byte("\n"))
	return bytes.ReplaceAll(text, []byte("\n"), []byte("\r\n"))
}

// stamp replaces placeholder with value in the file, and fails unless the
// placeholder occurs exactly want times: a manifest that silently keeps its
// placeholder would ship a wrong version.
func stamp(file, placeholder, value string, want int) error {
	data, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	if got := bytes.Count(data, []byte(placeholder)); got != want {
		return fmt.Errorf("%s: placeholder %q occurs %d times, expected %d", file, placeholder, got, want)
	}
	return os.WriteFile(file, bytes.ReplaceAll(data, []byte(placeholder), []byte(value)), 0o644)
}

// sourceTime is the timestamp given to every file in the plugin archive and
// in the bundles, so that the same sources produce the same archives. It is
// the time of the last commit, or of SOURCE_DATE_EPOCH if that is set, or a
// fixed date when neither is available.
func sourceTime() time.Time {
	epoch := os.Getenv("SOURCE_DATE_EPOCH")
	if epoch == "" {
		if out, err := exec.Command("git", "log", "-1", "--format=%ct").Output(); err == nil {
			epoch = strings.TrimSpace(string(out))
		}
	}
	var sec int64
	if _, err := fmt.Sscanf(epoch, "%d", &sec); err == nil && sec > 0 {
		return time.Unix(sec, 0).UTC()
	}
	return time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
}

// zipDir writes the files under dir to a zip archive with dir's contents at
// the top level. Names are sorted and every file carries the same timestamp,
// so the result depends only on the contents.
func zipDir(dir, archive string, modified time.Time) error {
	var names []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			rel, err := filepath.Rel(dir, p)
			if err != nil {
				return err
			}
			names = append(names, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return fmt.Errorf("%s: nothing to archive", dir)
	}
	sort.Strings(names)

	f, err := os.Create(archive)
	if err != nil {
		return err
	}
	w := zip.NewWriter(f)
	for _, name := range names {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: modified}
		h.SetMode(0o644)
		if path.Ext(name) == ".exe" {
			h.SetMode(0o755)
		}
		dst, err := w.CreateHeader(h)
		if err == nil {
			err = copyInto(dst, filepath.Join(dir, filepath.FromSlash(name)))
		}
		if err != nil {
			f.Close()
			return fmt.Errorf("archiving %s: %w", name, err)
		}
	}
	return errors.Join(w.Close(), f.Close())
}

func copyInto(dst io.Writer, file string) error {
	src, err := os.Open(file)
	if err != nil {
		return err
	}
	defer src.Close()
	_, err = io.Copy(dst, src)
	return err
}

func copyFile(from, to string) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	dst, err := os.Create(to)
	if err != nil {
		return err
	}
	return errors.Join(copyInto(dst, from), dst.Close())
}

// copyTree copies the regular files under from to the same relative paths
// under to. A server/ directory at the top is left out: the release puts its
// own build of the server there.
func copyTree(from, to string) error {
	return filepath.WalkDir(from, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, p)
		if err != nil {
			return err
		}
		if d.IsDir() {
			if filepath.ToSlash(rel) == "server" {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() {
			return nil
		}
		return copyFile(p, filepath.Join(to, rel))
	})
}

func sha256File(file string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
