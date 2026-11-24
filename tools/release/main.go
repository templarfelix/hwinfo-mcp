// Command release builds everything a release publishes into one directory:
//
//	hwinfo-mcp-windows-amd64.exe   the server
//	hwinfo-mcp-windows-arm64.exe
//	hwinfo-plugin.zip              the Claude Code plugin: skills, MCP
//	                               configuration and the amd64 server
//	hwinfo-mcp.mcpb                the MCP bundle for Claude Desktop and the
//	                               MCP Registry
//	server.json                    the MCP Registry entry for that bundle
//	SHA256SUMS
//
// CI runs it on a tag. Running it by hand produces the same files, which is
// how the plugin and the bundle are tested before a release:
//
//	go run ./tools/release -version v1.2.3 -out dist
//
// It must be started from the repository root. Packing the bundle uses the
// official mcpb tool through npx, so it needs Node.js; pass -mcpb "" to leave
// the bundle and server.json out.
package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
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
)

var versionRE = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$`)

func main() {
	log.SetFlags(0)
	log.SetPrefix("release: ")

	version := flag.String("version", "", "version to build, as a tag: v1.2.3")
	out := flag.String("out", "dist", "directory to write the release files to")
	mcpb := flag.String("mcpb", "npx --yes @anthropic-ai/mcpb@2.1.2", `command that runs the mcpb tool; "" skips the bundle and server.json`)
	flag.Parse()

	if !versionRE.MatchString(*version) {
		log.Fatalf("-version %q is not of the form v1.2.3", *version)
	}
	if _, err := os.Stat("go.mod"); err != nil {
		log.Fatal("run this from the repository root")
	}
	if err := run(*version, *out, strings.Fields(*mcpb)); err != nil {
		log.Fatal(err)
	}
}

func run(version, out string, mcpb []string) error {
	plain := strings.TrimPrefix(version, "v")
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	out, err := filepath.Abs(out)
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

	// The MCP bundle and its registry entry.
	if len(mcpb) > 0 {
		bundleDir := filepath.Join(stage, "bundle")
		for _, name := range []string{"manifest.json", bundleIcon} {
			if err := copyFile(filepath.Join("mcpb", name), filepath.Join(bundleDir, name)); err != nil {
				return err
			}
		}
		if err := stamp(filepath.Join(bundleDir, "manifest.json"), devVersion, plain, 1); err != nil {
			return err
		}
		if err := copyFile(exe["amd64"], filepath.Join(bundleDir, filepath.FromSlash(serverInArchive))); err != nil {
			return err
		}
		bundle := filepath.Join(out, "hwinfo-mcp.mcpb")
		for _, args := range [][]string{{"validate", bundleDir}, {"pack", bundleDir, bundle}} {
			tool := exec.Command(mcpb[0], append(mcpb[1:len(mcpb):len(mcpb)], args...)...)
			if err := runCommand(tool); err != nil {
				return fmt.Errorf("mcpb %s: %w", args[0], err)
			}
		}
		artifacts = append(artifacts, "hwinfo-mcp.mcpb")

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

// sourceTime is the timestamp given to every file in the plugin archive, so
// that the same sources produce the same archive. It is the time of the last
// commit, or of SOURCE_DATE_EPOCH if that is set, or a fixed date when
// neither is available.
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
