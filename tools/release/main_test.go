package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestStamp(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "manifest.json")
	write(t, dir, "manifest.json", `{"version": "0.0.0-dev", "url": "https://example.com/v0.0.0-dev/x"}`)

	// The count is part of the contract: one occurrence expected, two found.
	if err := stamp(file, devVersion, "1.2.3", 1); err == nil {
		t.Fatal("stamp accepted a file with a different number of placeholders than expected")
	}
	if err := stamp(file, devVersion, "1.2.3", 2); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(file)
	if want := `{"version": "1.2.3", "url": "https://example.com/v1.2.3/x"}`; string(got) != want {
		t.Errorf("stamped file = %s, want %s", got, want)
	}
	// A manifest that has lost its placeholder must stop the release.
	if err := stamp(file, devVersion, "1.2.4", 1); err == nil {
		t.Error("stamp accepted a file without the placeholder")
	}
}

func TestCopyTreeLeavesTheServerDirectoryOut(t *testing.T) {
	from, to := t.TempDir(), t.TempDir()
	write(t, from, ".claude-plugin/plugin.json", "{}")
	write(t, from, ".mcp.json", "{}")
	write(t, from, "skills/setup/SKILL.md", "skill")
	write(t, from, "skills/server/SKILL.md", "a skill that happens to be called server")
	write(t, from, "server/hwinfo-mcp.exe", "a stale local build")

	if err := copyTree(from, to); err != nil {
		t.Fatal(err)
	}
	var got []string
	filepath.WalkDir(to, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(to, p)
			got = append(got, filepath.ToSlash(rel))
		}
		return nil
	})
	want := []string{".claude-plugin/plugin.json", ".mcp.json", "skills/server/SKILL.md", "skills/setup/SKILL.md"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("copied %q, want %q", got, want)
	}
}

func TestZipDir(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "skills/b/SKILL.md", "second")
	write(t, dir, ".claude-plugin/plugin.json", `{"name": "hwinfo"}`)
	write(t, dir, "server/hwinfo-mcp.exe", "MZ")
	write(t, dir, "skills/a/SKILL.md", "first")

	when := time.Date(2026, 11, 23, 20, 38, 15, 0, time.UTC)
	out := t.TempDir()
	first, second := filepath.Join(out, "1.zip"), filepath.Join(out, "2.zip")
	for _, archive := range []string{first, second} {
		if err := zipDir(dir, archive, when); err != nil {
			t.Fatal(err)
		}
	}

	a, _ := os.ReadFile(first)
	b, _ := os.ReadFile(second)
	if !bytes.Equal(a, b) {
		t.Error("the same directory produced two different archives")
	}

	r, err := zip.OpenReader(first)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	var names []string
	for _, f := range r.File {
		names = append(names, f.Name)
		if strings.Contains(f.Name, `\`) {
			t.Errorf("%q uses a backslash; zip paths use forward slashes", f.Name)
		}
		if !f.Modified.Equal(when) {
			t.Errorf("%s is dated %v, want %v", f.Name, f.Modified, when)
		}
	}
	// The manifest must sit at the top level, where Claude Code looks for it.
	want := []string{".claude-plugin/plugin.json", "server/hwinfo-mcp.exe", "skills/a/SKILL.md", "skills/b/SKILL.md"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("archive holds %q, want %q", names, want)
	}

	f, err := r.Open("skills/a/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	content, _ := io.ReadAll(f)
	if string(content) != "first" {
		t.Errorf("content = %q", content)
	}

	if err := zipDir(t.TempDir(), filepath.Join(out, "empty.zip"), when); err == nil {
		t.Error("zipDir made an archive out of an empty directory")
	}
}

func TestVersionPattern(t *testing.T) {
	for version, want := range map[string]bool{
		"v0.2.0":      true,
		"v1.12.3":     true,
		"v0.2.0-rc.1": true,
		"v0.0.0-ci":   true,
		"0.2.0":       false,
		"v0.2":        false,
		"v0.2.0 ":     false,
		"":            false,
	} {
		if got := versionRE.MatchString(version); got != want {
			t.Errorf("version %q: accepted = %v, want %v", version, got, want)
		}
	}
}

// The manifests in the repository must carry the placeholders the release
// tool replaces, the expected number of times.
func TestRepositoryManifestsCarryThePlaceholders(t *testing.T) {
	for file, want := range map[string]int{
		"../../plugin/.claude-plugin/plugin.json": 1,
		"../../mcpb/manifest.json":                1,
		"../../mcpb/server.json":                  2,
	} {
		data, err := os.ReadFile(filepath.FromSlash(file))
		if err != nil {
			t.Fatal(err)
		}
		if got := bytes.Count(data, []byte(devVersion)); got != want {
			t.Errorf("%s: %q occurs %d times, want %d", file, devVersion, got, want)
		}
	}
	entry, err := os.ReadFile(filepath.FromSlash("../../mcpb/server.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := bytes.Count(entry, []byte(devSHA)); got != 1 {
		t.Errorf("mcpb/server.json: the digest placeholder occurs %d times, want 1", got)
	}
}

// The bundle's manifest must name the icon the release tool packs, and that
// file must be a square PNG large enough for a client to scale down.
func TestBundleIcon(t *testing.T) {
	data, err := os.ReadFile(filepath.FromSlash("../../mcpb/manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Icon string `json:"icon"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Icon != bundleIcon {
		t.Fatalf("mcpb/manifest.json names the icon %q, but the release packs %q", manifest.Icon, bundleIcon)
	}

	f, err := os.Open(filepath.FromSlash("../../mcpb/" + bundleIcon))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		t.Fatalf("mcpb/%s is not a PNG: %v", bundleIcon, err)
	}
	if cfg.Width != cfg.Height || cfg.Width < 512 {
		t.Errorf("mcpb/%s is %dx%d, want a square of at least 512 pixels", bundleIcon, cfg.Width, cfg.Height)
	}
}

// The plugin carries the same icon as the bundle, where a plugin directory
// looks for one. Two copies of a picture drift apart unless something holds
// them together.
func TestPluginIconIsTheBundlesIcon(t *testing.T) {
	bundle, err := os.ReadFile(filepath.FromSlash("../../mcpb/" + bundleIcon))
	if err != nil {
		t.Fatal(err)
	}
	plugin, err := os.ReadFile(filepath.FromSlash("../../plugin/.claude-plugin/icon.png"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bundle, plugin) {
		t.Error("plugin/.claude-plugin/icon.png differs from mcpb/icon.png; copy one over the other")
	}
}
