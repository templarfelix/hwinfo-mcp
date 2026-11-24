package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
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

// zipOf returns a zip archive with the given entries.
func zipOf(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range entries {
		f, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// samplePackage is an archive shaped like HWiNFO's portable package. Its
// executables are not programs, so nothing can start one by accident.
func samplePackage(t *testing.T) []byte {
	t.Helper()
	entries := map[string][]byte{}
	for _, name := range []string{"HWiNFO32.exe", "HWiNFO64.exe", "HWiNFO_ARM64.exe"} {
		entries[name] = []byte("not a program: a stand-in for " + name)
	}
	return zipOf(t, entries)
}

// pinFor pins pkg, to be downloaded from url.
func pinFor(pkg []byte, url string) hwinfoPackage {
	sum := sha256.Sum256(pkg)
	return hwinfoPackage{
		Version: "9.99",
		File:    "hwi_999.zip",
		Size:    int64(len(pkg)),
		SHA256:  hex.EncodeToString(sum[:]),
		URL:     url,
	}
}

func TestObtain(t *testing.T) {
	pkg := samplePackage(t)
	tampered := append([]byte{pkg[0] ^ 0xff}, pkg[1:]...)
	carrying := zipOf(t, map[string][]byte{"manifest.json": []byte("{}"), "hwinfo/hwi_999.zip": pkg})
	lean := zipOf(t, map[string][]byte{"manifest.json": []byte("{}")})

	var hits atomic.Int32
	mux := http.NewServeMux()
	serve := func(pattern string, content []byte) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, _ *http.Request) {
			hits.Add(1)
			w.Write(content)
		})
	}
	serve("/mirror/hwi_999.zip", pkg)
	serve("/tampered/hwi_999.zip", tampered)
	serve("/longer/hwi_999.zip", append(append([]byte{}, pkg...), 'x'))
	serve("/carrying.mcpb", carrying)
	serve("/lean.mcpb", lean)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// kept reports what obtain left in dir.
	kept := func(dir string) []string {
		var names []string
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return names
	}

	t.Run("downloads once and then uses what it kept", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "cache")
		pin := pinFor(pkg, srv.URL+"/mirror/hwi_999.zip")
		hits.Store(0)
		for range 2 {
			file, err := obtain(srv.Client(), pin, dir)
			if err != nil {
				t.Fatal(err)
			}
			if got, _ := os.ReadFile(file); !bytes.Equal(got, pkg) || filepath.Dir(file) != dir {
				t.Fatalf("%s is not the package in the directory asked for", file)
			}
		}
		if got := hits.Load(); got != 1 {
			t.Errorf("the mirror was asked %d times, want once", got)
		}
		if got := kept(dir); !reflect.DeepEqual(got, []string{"hwi_999.zip"}) {
			t.Errorf("the directory holds %q, want only the package", got)
		}
	})

	t.Run("replaces a kept file that is not the pinned one", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "hwi_999.zip", "left over from another version")
		file, err := obtain(srv.Client(), pinFor(pkg, srv.URL+"/mirror/hwi_999.zip"), dir)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := os.ReadFile(file); !bytes.Equal(got, pkg) {
			t.Error("the stale file was used")
		}
	})

	t.Run("takes the package out of a published bundle when the mirror dropped it", func(t *testing.T) {
		dir := t.TempDir()
		pin := pinFor(pkg, srv.URL+"/gone/hwi_999.zip")
		pin.Bundle = srv.URL + "/carrying.mcpb"
		file, err := obtain(srv.Client(), pin, dir)
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := os.ReadFile(file); !bytes.Equal(got, pkg) {
			t.Error("what came out of the bundle is not the package")
		}
		if got := kept(dir); !reflect.DeepEqual(got, []string{"hwi_999.zip"}) {
			t.Errorf("the directory holds %q, want only the package", got)
		}
	})

	// Whatever does not add up is an error, and nothing of it is kept.
	for name, tt := range map[string]struct {
		url, bundle string
		wantErr     []string
	}{
		"a different file of the same size":  {"/tampered/hwi_999.zip", "", []string{"is pinned", "point mcpb/hwinfo.json at it"}},
		"a larger file":                      {"/longer/hwi_999.zip", "", []string{"larger than"}},
		"mirror gone and no bundle":          {"/gone/hwi_999.zip", "", []string{"404", "No published bundle carries it yet"}},
		"mirror gone, bundle without it":     {"/gone/hwi_999.zip", "/lean.mcpb", []string{"404", "holds no hwinfo/hwi_999.zip"}},
		"mirror and bundle gone":             {"/gone/hwi_999.zip", "/gone.mcpb", []string{"hwi_999.zip: 404", "gone.mcpb: 404"}},
		"mirror tampered, bundle carries it": {"/tampered/hwi_999.zip", "/carrying.mcpb", nil},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			pin := pinFor(pkg, srv.URL+tt.url)
			if tt.bundle != "" {
				pin.Bundle = srv.URL + tt.bundle
			}
			file, err := obtain(srv.Client(), pin, dir)
			if tt.wantErr == nil {
				// The bundle makes up for a mirror that serves something else.
				if got, _ := os.ReadFile(file); err != nil || !bytes.Equal(got, pkg) {
					t.Errorf("obtain: %v; the package was not taken from the bundle", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("obtain returned %s", file)
			}
			for _, want := range tt.wantErr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error = %v, want it to mention %q", err, want)
				}
			}
			if got := kept(dir); len(got) != 0 {
				t.Errorf("the directory holds %q after a failure, want nothing", got)
			}
		})
	}
}

func TestPin(t *testing.T) {
	// The pin in the repository is what a release is built from.
	pin, err := readPin(filepath.FromSlash("../../mcpb/hwinfo.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pin.URL, pin.File) {
		t.Errorf("the url %s does not end in the file %s it is pinned as", pin.URL, pin.File)
	}
	if pin.Bundle != "" && !strings.HasSuffix(pin.Bundle, "/"+fullBundleName) {
		t.Errorf("bundle = %s, want a published %s", pin.Bundle, fullBundleName)
	}
	// The READMEs say which package the bundle carries. A new pin has to
	// reach all three of them.
	for _, name := range []string{"README.md", "README.pt-BR.md", "README.es.md"} {
		readme, err := os.ReadFile(filepath.Join("..", "..", name))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"`" + pin.File + "`", " " + pin.Version + ",", pin.SHA256, fullBundleName} {
			if !bytes.Contains(readme, []byte(want)) {
				t.Errorf("%s does not mention %q, which mcpb/hwinfo.json pins", name, want)
			}
		}
	}

	good := pinFor([]byte("x"), "https://example.com/hwi_999.zip")
	if err := good.check(); err != nil {
		t.Fatalf("a complete pin was refused: %v", err)
	}
	for name, change := range map[string]func(*hwinfoPackage){
		"no version":                       func(p *hwinfoPackage) { p.Version = "" },
		"a path as the file":               func(p *hwinfoPackage) { p.File = "../hwi_999.zip" },
		"a file the helper would not find": func(p *hwinfoPackage) { p.File = "HWiNFO.zip" },
		"no size":                          func(p *hwinfoPackage) { p.Size = 0 },
		"an upper-case digest":             func(p *hwinfoPackage) { p.SHA256 = strings.ToUpper(p.SHA256) },
		"a short digest":                   func(p *hwinfoPackage) { p.SHA256 = p.SHA256[:40] },
		"a plain http url":                 func(p *hwinfoPackage) { p.URL = "http://example.com/hwi_999.zip" },
		"a plain http bundle":              func(p *hwinfoPackage) { p.Bundle = "http://example.com/x.mcpb" },
	} {
		bad := good
		change(&bad)
		if err := bad.check(); err == nil {
			t.Errorf("%s: the pin was accepted", name)
		}
	}

	// A misspelt field must not pass for an absent one.
	dir := t.TempDir()
	write(t, dir, "hwinfo.json", `{"version": "9.99", "file": "hwi_999.zip", "size": 1, "sha256": "`+good.SHA256+`", "url": "https://example.com/hwi_999.zip", "bundel": ""}`)
	if _, err := readPin(filepath.Join(dir, "hwinfo.json")); err == nil {
		t.Error("a pin with an unknown field was accepted")
	}
}

func TestCRLF(t *testing.T) {
	if got := string(crlf([]byte("one\ntwo\r\nthree\n"))); got != "one\r\ntwo\r\nthree\r\n" {
		t.Errorf("crlf = %q", got)
	}
}

// lineEndings reports how many lines of text end in CR LF and how many in a
// bare LF.
func lineEndings(text []byte) (crlf, bare int) {
	crlf = bytes.Count(text, []byte("\r\n"))
	return crlf, bytes.Count(text, []byte("\n")) - crlf
}

// The sources of the hwinfo folder: this project's own files, which Windows
// has to read whatever its code page, and the places the release tool writes
// into.
func TestRepositoryHWiNFOSources(t *testing.T) {
	sources := filepath.FromSlash("../../mcpb/hwinfo")
	entries, err := os.ReadDir(sources)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !reflect.DeepEqual(names, hwinfoFiles) {
		t.Errorf("mcpb/hwinfo holds %q, but the release packs %q", names, hwinfoFiles)
	}
	for _, name := range hwinfoFiles {
		data, err := os.ReadFile(filepath.Join(sources, name))
		if err != nil {
			t.Fatal(err)
		}
		for i, b := range data {
			if b > 0x7e || (b < 0x20 && b != '\n' && b != '\r') {
				t.Errorf("mcpb/hwinfo/%s: byte %#x at offset %d; keep the file in plain ASCII", name, b, i)
				break
			}
		}
	}

	settings, err := os.ReadFile(filepath.Join(sources, "HWiNFO64.INI"))
	if err != nil {
		t.Fatal(err)
	}
	// Shared memory is what the server reads. Without the last one, closing
	// the Sensors window ends HWiNFO, and the readings with it.
	for _, want := range []string{"[Settings]", "SensorsOnly=1", "SensorsSM=1", "MinimalizeSensorsClose=1"} {
		if !bytes.Contains(settings, []byte(want)) {
			t.Errorf("HWiNFO64.INI lacks %s", want)
		}
	}
	// The helper promises that HWiNFO is not made to start with Windows.
	if bytes.Contains(settings, []byte("Autorun")) {
		t.Error("HWiNFO64.INI sets Autorun; whether HWiNFO starts with Windows is the user's choice")
	}

	manifest, err := os.ReadFile(filepath.FromSlash("../../mcpb/manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := bytes.Count(manifest, []byte(noteAnchor)); got != 1 {
		t.Errorf("mcpb/manifest.json: the sentence the note about HWiNFO goes after occurs %d times, want 1", got)
	}
}

func TestAddHWiNFO(t *testing.T) {
	pkg := samplePackage(t)
	pin := pinFor(pkg, "https://example.com/hwi_999.zip")
	scratch := t.TempDir()
	write(t, scratch, "hwi_999.zip", string(pkg))
	write(t, scratch, "hwinfo-mcp.exe", "the server")
	pkgFile, server := filepath.Join(scratch, "hwi_999.zip"), filepath.Join(scratch, "hwinfo-mcp.exe")

	longDescription := func(dir string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
		if err != nil {
			t.Fatal(err)
		}
		var manifest struct {
			Version         string `json:"version"`
			LongDescription string `json:"long_description"`
		}
		if err := json.Unmarshal(data, &manifest); err != nil {
			t.Fatalf("%s: the manifest is not JSON: %v", dir, err)
		}
		if manifest.Version != "1.2.3" {
			t.Errorf("%s: version = %q", dir, manifest.Version)
		}
		return manifest.LongDescription
	}

	// The bundle without HWiNFO says nothing about it and has no such folder.
	lean := filepath.Join(scratch, "lean")
	if err := stageBundle(lean, filepath.FromSlash("../../mcpb"), "1.2.3", server); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(longDescription(lean), "also carries") {
		t.Error("the bundle without HWiNFO announces one")
	}
	if _, err := os.Stat(filepath.Join(lean, hwinfoDir)); err == nil {
		t.Error("the bundle without HWiNFO has a hwinfo folder")
	}

	full := filepath.Join(scratch, "full")
	if err := stageBundle(full, filepath.FromSlash("../../mcpb"), "1.2.3", server); err != nil {
		t.Fatal(err)
	}
	if err := addHWiNFO(full, filepath.FromSlash("../../mcpb/hwinfo"), pin, pkgFile); err != nil {
		t.Fatal(err)
	}

	// The package is what was obtained, byte for byte.
	if got, _ := os.ReadFile(filepath.Join(full, hwinfoDir, pin.File)); !bytes.Equal(got, pkg) {
		t.Error("the package in the bundle differs from the one obtained")
	}
	entries, err := os.ReadDir(filepath.Join(full, hwinfoDir))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if want := []string{"HWiNFO64.INI", "README.txt", "Setup-HWiNFO.cmd", "hwi_999.zip"}; !reflect.DeepEqual(names, want) {
		t.Errorf("the hwinfo folder holds %q, want %q", names, want)
	}
	for _, name := range hwinfoFiles {
		data, _ := os.ReadFile(filepath.Join(full, hwinfoDir, name))
		if with, bare := lineEndings(data); with == 0 || bare != 0 {
			t.Errorf("%s: %d lines end in CR LF and %d in a bare LF; Windows reads this file as it is", name, with, bare)
		}
	}

	// The notice names the package next to it and keeps apart whose is what.
	notice, _ := os.ReadFile(filepath.Join(full, hwinfoDir, "README.txt"))
	for _, want := range []string{pin.File, "version " + pin.Version, pin.SHA256, "with its author's permission", "not by HWiNFO's author", "non-commercial"} {
		if !bytes.Contains(notice, []byte(want)) {
			t.Errorf("README.txt lacks %q", want)
		}
	}
	if bytes.Contains(notice, []byte("HWINFO_PACKAGE")) {
		t.Error("README.txt still carries a placeholder")
	}

	// So does the manifest, after what it said before.
	long := longDescription(full)
	for _, want := range []string{
		strings.TrimSuffix(noteAnchor, `"`) + "\n\nThis package also carries HWiNFO's portable package, version 9.99,",
		"included with its author's permission",
		"The settings file and the setup helper next to it are this project's own.",
		"Nothing in that folder runs unless you start it.",
	} {
		if !strings.Contains(long, want) {
			t.Errorf("long_description = %q, want it to contain %q", long, want)
		}
	}

	// A manifest that no longer ends the way the note expects stops the release.
	if err := addHWiNFO(full, filepath.FromSlash("../../mcpb/hwinfo"), pin, pkgFile); err == nil {
		t.Error("addHWiNFO added its note to a manifest a second time")
	}
}

// When the mirror has dropped the package, obtain takes it out of a published
// bundle. So the bundle has to hold it under the name obtain asks for.
func TestTheFullBundleCarriesThePackageWhereObtainLooksForIt(t *testing.T) {
	pkg := samplePackage(t)
	pin := pinFor(pkg, "https://example.com/hwi_999.zip")
	scratch := t.TempDir()
	write(t, scratch, "hwi_999.zip", string(pkg))
	write(t, scratch, "hwinfo-mcp.exe", "the server")

	full := filepath.Join(scratch, "full")
	if err := stageBundle(full, filepath.FromSlash("../../mcpb"), "1.2.3", filepath.Join(scratch, "hwinfo-mcp.exe")); err != nil {
		t.Fatal(err)
	}
	if err := addHWiNFO(full, filepath.FromSlash("../../mcpb/hwinfo"), pin, filepath.Join(scratch, "hwi_999.zip")); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(scratch, fullBundleName)
	if err := pack(full, bundle, nil); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(scratch, "out.zip")
	if err := extract(bundle, hwinfoDir+"/"+pin.File, out); err != nil {
		t.Fatal(err)
	}
	if err := pin.verify(out); err != nil {
		t.Errorf("the package taken out of the bundle is not the pinned one: %v", err)
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
