package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// These tests run the setup helper for real, on Windows, the way the bundle
// that carries HWiNFO ships it. No HWiNFO is involved: the package is a
// stand-in, and where a program has to start or to be running, it is this
// test binary under HWiNFO's name.

// standIn is the environment variable that turns this test binary into a
// stand-in for HWiNFO: "wait" keeps it running for a while, and "mark:" plus
// a file name makes it write that file and leave.
const standIn = "HWINFO_MCP_TEST_STAND_IN"

func TestMain(m *testing.M) {
	switch mode := os.Getenv(standIn); {
	case mode == "wait":
		time.Sleep(time.Minute)
		return
	case strings.HasPrefix(mode, "mark:"):
		os.WriteFile(strings.TrimPrefix(mode, "mark:"), []byte("started"), 0o644)
		return
	}
	os.Exit(m.Run())
}

// stagedHelper lays out the hwinfo folder of a bundle, with a stand-in
// package, under a path with a space in it like the one Claude Desktop
// installs extensions to. It returns the folder.
func stagedHelper(t *testing.T) string {
	t.Helper()
	pkg := samplePackage(t)
	scratch := t.TempDir()
	write(t, scratch, "hwi_999.zip", string(pkg))
	write(t, scratch, "hwinfo-mcp.exe", "the server")

	bundle := filepath.Join(scratch, "Claude Extensions", "local.mcpb.someone.hwinfo")
	if err := stageBundle(bundle, filepath.FromSlash("../../mcpb"), "1.2.3", filepath.Join(scratch, "hwinfo-mcp.exe")); err != nil {
		t.Fatal(err)
	}
	pin := pinFor(pkg, "https://example.com/hwi_999.zip")
	if err := addHWiNFO(bundle, filepath.FromSlash("../../mcpb/hwinfo"), pin, filepath.Join(scratch, "hwi_999.zip")); err != nil {
		t.Fatal(err)
	}
	return filepath.Join(bundle, hwinfoDir)
}

// runHelper runs the setup helper in folder with dest as the place to unpack
// to, and returns what it printed and its exit code. With no console to read
// a key from, the helper's closing pause returns at once. The helper is shown
// a Windows with no HWiNFO installed, whatever this machine has, unless env
// says otherwise.
func runHelper(t *testing.T, folder, dest string, env []string, args ...string) (string, int) {
	t.Helper()
	helper := filepath.Join(folder, "Setup-HWiNFO.cmd")
	cmd := exec.Command(os.Getenv("ComSpec"))
	// cmd.exe has quoting rules of its own: with /s it takes everything
	// between the outermost quotes after /c as the command.
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `/d /s /c ""` + helper + `" ` + strings.Join(args, " ") + `"`}
	programs := t.TempDir()
	cmd.Env = append(os.Environ(), "HWINFO_MCP_HWINFO_DIR="+dest, "ProgramFiles="+programs, "ProgramW6432="+programs)
	cmd.Env = append(cmd.Env, env...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatalf("running the helper: %v", err)
	}
	return out.String(), code
}

// onArm reports whether the helper takes this Windows for one on Arm, where
// it starts another executable and adds no settings file.
func onArm() bool {
	return strings.EqualFold(os.Getenv("PROCESSOR_ARCHITECTURE"), "ARM64") ||
		strings.EqualFold(os.Getenv("PROCESSOR_ARCHITEW6432"), "ARM64")
}

func hwinfoExe() string {
	if onArm() {
		return "HWiNFO_ARM64.exe"
	}
	return "HWiNFO64.exe"
}

// hwinfoRunning reports whether a process with one of HWiNFO's image names
// exists, which is what the helper looks at.
func hwinfoRunning(t *testing.T) bool {
	t.Helper()
	out, err := exec.Command("tasklist", "/NH").Output()
	if err != nil {
		t.Fatalf("tasklist: %v", err)
	}
	for _, name := range []string{"hwinfo64.exe", "hwinfo32.exe", "hwinfo_arm64.exe"} {
		if strings.Contains(strings.ToLower(string(out)), name) {
			return true
		}
	}
	return false
}

func TestSetupHelperUnpacksOnceAndKeepsWhatIsThere(t *testing.T) {
	folder := stagedHelper(t)
	// A destination with what a user name can bring: a space, an accent, an
	// ampersand and parentheses.
	dest := filepath.Join(t.TempDir(), "Jos\u00e9 & Ana (PC)", "hwinfo-mcp", "HWiNFO")

	out, code := runHelper(t, folder, dest, nil, "/nostart")
	if code != 0 {
		t.Fatalf("exit code %d:\n%s", code, out)
	}
	for _, name := range []string{"HWiNFO32.exe", "HWiNFO64.exe", "HWiNFO_ARM64.exe"} {
		got, err := os.ReadFile(filepath.Join(dest, name))
		if err != nil || string(got) != "not a program: a stand-in for "+name {
			t.Errorf("%s was not unpacked as it is in the package: %v", name, err)
		}
	}
	if !strings.Contains(out, "Unpacked HWiNFO into") {
		t.Errorf("the helper did not say what it unpacked:\n%s", out)
	}

	settings := filepath.Join(dest, "HWiNFO64.INI")
	shipped, _ := os.ReadFile(filepath.Join(folder, "HWiNFO64.INI"))
	if onArm() {
		if _, err := os.Stat(settings); err == nil {
			t.Error("a settings file was added on Arm, where its name is not known")
		}
		return
	}
	if got, err := os.ReadFile(settings); err != nil || !bytes.Equal(got, shipped) {
		t.Fatalf("the settings file was not put next to HWiNFO: %v", err)
	}
	if !strings.Contains(out, "Added the settings file") {
		t.Errorf("the helper did not say it added the settings file:\n%s", out)
	}

	// A second run finds HWiNFO and its settings there and touches neither:
	// by then they are the user's.
	if err := os.WriteFile(settings, []byte("[Settings]\r\nSensorsSM=0\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "HWiNFO64.exe"), []byte("a newer HWiNFO"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, code = runHelper(t, folder, dest, nil, "/nostart")
	if code != 0 {
		t.Fatalf("second run: exit code %d:\n%s", code, out)
	}
	if got, _ := os.ReadFile(settings); string(got) != "[Settings]\r\nSensorsSM=0\r\n" {
		t.Errorf("the second run replaced the settings file with %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dest, "HWiNFO64.exe")); string(got) != "a newer HWiNFO" {
		t.Error("the second run unpacked over the HWiNFO that was there")
	}
	if strings.Count(out, "left as it is") != 2 {
		t.Errorf("the second run did not say it left both alone:\n%s", out)
	}
}

// Where HWiNFO is installed, the copy in the bundle is one too many: the
// helper says so and leaves it packed, unless it is told to go ahead.
func TestSetupHelperLeavesAnInstalledHWiNFOAlone(t *testing.T) {
	folder := stagedHelper(t)
	programs := t.TempDir()
	write(t, programs, "HWiNFO64/"+hwinfoExe(), "an installed HWiNFO")
	installed := []string{"ProgramFiles=" + programs, "ProgramW6432=" + programs}

	dest := filepath.Join(t.TempDir(), "HWiNFO")
	out, code := runHelper(t, folder, dest, installed)
	if code != 0 || !strings.Contains(out, "HWiNFO is already installed on this PC") || !strings.Contains(out, "/portable") {
		t.Errorf("exit code %d; the helper did not say why it held back and how to go ahead:\n%s", code, out)
	}
	if _, err := os.Stat(dest); err == nil {
		t.Error("the helper unpacked its copy next to an installed HWiNFO")
	}

	// Either order of the arguments does.
	for _, args := range [][]string{{"/portable", "/nostart"}, {"/nostart", "/portable"}} {
		dest := filepath.Join(t.TempDir(), "HWiNFO")
		out, code := runHelper(t, folder, dest, installed, args...)
		if _, err := os.Stat(filepath.Join(dest, hwinfoExe())); code != 0 || err != nil {
			t.Errorf("%v: exit code %d, %v; the helper did not set its copy up when told to:\n%s", args, code, err, out)
		}
	}
}

func TestSetupHelperWithoutThePackage(t *testing.T) {
	folder := stagedHelper(t)
	if err := os.Remove(filepath.Join(folder, "hwi_999.zip")); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "HWiNFO")
	out, code := runHelper(t, folder, dest, nil, "/nostart")
	if code != 1 || !strings.Contains(out, "was not found") {
		t.Errorf("exit code %d, want 1 and an explanation:\n%s", code, out)
	}
	if _, err := os.Stat(dest); err == nil {
		t.Error("the helper created the destination although it had nothing to unpack")
	}
}

// prepared returns a destination that already holds HWiNFO and its settings,
// where "HWiNFO" is this test binary: started, it writes the returned marker
// file and leaves.
func prepared(t *testing.T) (dest, marker string, env []string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dest = filepath.Join(t.TempDir(), "Jos\u00e9 & Ana (PC)", "HWiNFO")
	if err := copyFile(self, filepath.Join(dest, hwinfoExe())); err != nil {
		t.Fatal(err)
	}
	write(t, dest, "HWiNFO64.INI", "[Settings]\r\n")
	marker = filepath.Join(t.TempDir(), "started")
	return dest, marker, []string{standIn + "=mark:" + marker}
}

func TestSetupHelperStartsNothingWhileAHWiNFOIsRunning(t *testing.T) {
	folder := stagedHelper(t)
	dest, marker, env := prepared(t)

	// A HWiNFO that is already running, as far as the helper can tell.
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	running := filepath.Join(t.TempDir(), "HWiNFO64.exe")
	if err := copyFile(self, running); err != nil {
		t.Fatal(err)
	}
	other := exec.Command(running)
	other.Env = append(os.Environ(), standIn+"=wait")
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	gone := make(chan struct{})
	go func() {
		other.Wait()
		close(gone)
	}()
	t.Cleanup(func() {
		other.Process.Kill()
		<-gone
	})

	out, code := runHelper(t, folder, dest, env)
	if code != 0 || !strings.Contains(out, "is already running, so nothing was started") {
		t.Errorf("exit code %d; the helper did not hold back:\n%s", code, out)
	}
	time.Sleep(2 * time.Second)
	if _, err := os.Stat(marker); err == nil {
		t.Error("the helper started HWiNFO although one was running")
	}
	// Nor does it close the one that runs.
	select {
	case <-gone:
		t.Error("the HWiNFO that was running is gone after the helper ran")
	default:
	}
}

func TestSetupHelperStartsHWiNFO(t *testing.T) {
	if hwinfoRunning(t) {
		t.Skip("a HWiNFO is running on this machine, and the helper starts nothing next to it")
	}
	folder := stagedHelper(t)
	dest, marker, env := prepared(t)

	out, code := runHelper(t, folder, dest, env)
	if code != 0 || !strings.Contains(out, "HWiNFO is starting") {
		t.Fatalf("exit code %d:\n%s", code, out)
	}
	for range 100 {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Errorf("the helper said it started HWiNFO, but nothing ran:\n%s", out)
}
