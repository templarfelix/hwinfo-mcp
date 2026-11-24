package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/templarfelix/hwinfo-mcp/internal/hwinfo"
	"github.com/templarfelix/hwinfo-mcp/internal/hwinfo/hwinfotest"
)

// degC is spelled with an escape so the comparison does not depend on how
// this file or a terminal encodes the degree sign.
const degC = "\u00b0C"

// pollTime is when the fixture's sensors were last polled, and clock is two
// seconds later: a fresh snapshot, as HWiNFO's default polling produces.
var pollTime = time.Date(2026, 11, 23, 16, 31, 43, 0, time.UTC)

func clock() time.Time { return pollTime.Add(2 * time.Second) }

// fixture is a small system with translated labels, the way a Portuguese
// HWiNFO reports them: one CPU spread over two sensors, one GPU, a mainboard.
func fixture() hwinfotest.Image {
	return hwinfotest.Image{
		PollTime:      pollTime.Unix(),
		PollingPeriod: 2000,
		Sensors: []hwinfotest.Sensor{
			{Name: "CPU [#0]: AMD Ryzen 9 9950X3D"},
			{Name: "CPU [#0]: AMD Ryzen 9 9950X3D: Enhanced"},
			{Name: "dGPU [#0]: NVIDIA GeForce RTX 5080"},
			{Name: "GIGABYTE X870E AORUS ELITE WIFI7 (ITE IT8696E)"},
			{Name: "System: GIGABYTE X870E AORUS ELITE WIFI7", UserName: "Sistema: GIGABYTE X870E AORUS ELITE WIFI7"},
		},
		Readings: []hwinfotest.Reading{
			{Type: hwinfotest.Usage, Sensor: 0, Label: "Total CPU Usage", UserLabel: "Uso total da CPU", Unit: "%", Value: 57.2461, Min: 1, Max: 100, Avg: 40},
			{Type: hwinfotest.Temperature, Sensor: 1, Label: "CPU (Tctl/Tdie)", Unit: degC, Value: 73, Min: 45.5, Max: 80.125, Avg: 70.0004},
			{Type: hwinfotest.Temperature, Sensor: 1, Label: "CPU Die (average)", UserLabel: "Caso CPU (m\u00e9dia)", Unit: degC, Value: 70.66064453125, Min: 44, Max: 78, Avg: 68},
			{Type: hwinfotest.Temperature, Sensor: 1, Label: "CPU CCD1 (Tdie)", Unit: degC, Value: 65.25, Min: 40, Max: 80.125, Avg: 60},
			{Type: hwinfotest.Temperature, Sensor: 1, Label: "CPU CCD2 (Tdie)", Unit: degC, Value: 70.5, Min: 41, Max: 77.25, Avg: 62},
			{Type: hwinfotest.Temperature, Sensor: 1, Label: "Core0 (CCD1)", Unit: degC, Value: 61.31, Min: 40, Max: 66, Avg: 55},
			{Type: hwinfotest.Temperature, Sensor: 1, Label: "Core10 (CCD2)", Unit: degC, Value: 69.219, Min: 41, Max: 73, Avg: 57},
			{Type: hwinfotest.Power, Sensor: 1, Label: "CPU Package Power", UserLabel: "Pot\u00eancia total da CPU", Unit: "W", Value: 166.3, Min: 38, Max: 190, Avg: 120},
			{Type: hwinfotest.Other, Sensor: 1, Label: "Thermal Throttling (HTC)", UserLabel: "Estrangulamento t\u00e9rmico (HTC)", Unit: "Yes/No"},
			{Type: hwinfotest.Temperature, Sensor: 2, Label: "GPU Temperature", UserLabel: "Temperatura GPU", Unit: degC, Value: 59.355, Min: 40, Max: 66, Avg: 55},
			{Type: hwinfotest.Temperature, Sensor: 2, Label: "GPU Memory Junction Temperature", UserLabel: "Temperatura de jun\u00e7\u00e3o da mem\u00f3ria GPU", Unit: degC, Value: 66, Min: 44, Max: 70, Avg: 60},
			{Type: hwinfotest.Temperature, Sensor: 2, Label: "GPU Hot Spot Temperature", UserLabel: "Temperatura de ponto quente da GPU", Unit: degC, Value: 70.594, Min: 50, Max: 83, Avg: 66},
			{Type: hwinfotest.Fan, Sensor: 2, Label: "GPU Fan1", UserLabel: "GPU Ventilador1", Unit: "RPM", Value: 1330, Min: 0, Max: 1800, Avg: 900},
			{Type: hwinfotest.Power, Sensor: 2, Label: "GPU Power", UserLabel: "GPU Pot\u00eancia", Unit: "W", Value: 220.153, Min: 20, Max: 310, Avg: 150},
			{Type: hwinfotest.Power, Sensor: 2, Label: "GPU Power Limit (rated)", UserLabel: "Limite de pot\u00eancia da GPU (nominal)", Unit: "W", Value: 360, Min: 360, Max: 360, Avg: 360},
			{Type: hwinfotest.Other, Sensor: 2, Label: "Performance Limit - Thermal", UserLabel: "Limite de desempenho - T\u00e9rmico", Unit: "Yes/No", Max: 1},
			{Type: hwinfotest.Temperature, Sensor: 3, Label: "CPU", Unit: degC, Value: 73, Min: 45, Max: 80, Avg: 70},
			{Type: hwinfotest.Fan, Sensor: 3, Label: "System 2", UserLabel: "Sistema 2", Unit: "RPM", Value: 1310, Min: 800, Max: 1500, Avg: 1100},
			{Type: hwinfotest.Other, Sensor: 4, Label: "Physical Memory Load", UserLabel: "Carga da mem\u00f3ria f\u00edsica", Unit: "%", Value: 94.7, Min: 50, Max: 97, Avg: 90},
		},
	}
}

// from returns a source that serves the block described by im, as if it had
// been copied under HWiNFO's mutex.
func from(t *testing.T, im hwinfotest.Image) source {
	t.Helper()
	snap, err := hwinfo.Parse(im.Bytes())
	if snap == nil {
		t.Fatalf("Parse: %v", err)
	}
	snap.Locked = true
	return func() (*hwinfo.Snapshot, error) { return snap, err }
}

func failing(err error) source {
	return func() (*hwinfo.Snapshot, error) { return nil, err }
}

// connect runs the server around src and returns a client connected to it.
func connect(t *testing.T, src source) *mcp.ClientSession {
	t.Helper()
	return connectAt(t, src, clock, sleepFor)
}

// connectAt is connect with a clock and a wait of the test's choosing.
func connectAt(t *testing.T, src source, now func() time.Time, sleep func(context.Context, time.Duration) error) *mcp.ClientSession {
	t.Helper()
	return connectTo(t, newServerAt(src, now, sleep, ""))
}

// connectTo returns a client connected to server.
func connectTo(t *testing.T, server *mcp.Server) *mcp.ClientSession {
	t.Helper()
	serverSide, clientSide := mcp.NewInMemoryTransports()
	ss, err := server.Connect(t.Context(), serverSide, nil)
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil).Connect(t.Context(), clientSide, nil)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	t.Cleanup(func() {
		cs.Close()
		ss.Wait()
	})
	return cs
}

// call invokes a tool and returns its raw result.
func call(t *testing.T, cs *mcp.ClientSession, tool string, args any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error: %v", tool, err)
	}
	return res
}

// text returns the text content of a result.
func text(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) != 1 {
		t.Fatalf("result has %d content items, want 1", len(res.Content))
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content is %T, want text", res.Content[0])
	}
	return tc.Text
}

// output invokes a tool that must succeed and decodes what it returned. The
// structured content and the text content must carry the same JSON.
func output[T any](t *testing.T, cs *mcp.ClientSession, tool string, args any) (out T, raw string) {
	t.Helper()
	res := call(t, cs, tool, args)
	raw = text(t, res)
	if res.IsError {
		t.Fatalf("%s failed: %s", tool, raw)
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("%s: text content is not the output JSON: %v\n%s", tool, err, raw)
	}

	structured, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	if json.Unmarshal(structured, &a) != nil || json.Unmarshal([]byte(raw), &b) != nil || !reflect.DeepEqual(a, b) {
		t.Errorf("%s: structured content and text content differ:\n%s\n%s", tool, structured, raw)
	}
	return out, raw
}

func f(v float64) *float64 { return &v }

func TestToolList(t *testing.T) {
	cs := connect(t, from(t, fixture()))
	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
		a := tool.Annotations
		if a == nil || !a.ReadOnlyHint || !a.IdempotentHint || a.OpenWorldHint == nil || *a.OpenWorldHint {
			t.Errorf("%s: annotations %+v do not declare a read-only, closed-world tool", tool.Name, a)
		}
		if tool.Description == "" || tool.OutputSchema == nil {
			t.Errorf("%s: missing description or output schema", tool.Name)
		}
	}
	sort.Strings(names)
	if want := []string{"get_readings", "hwinfo_status", "list_sensors", "sample_readings", "thermal_overview"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("tools = %q, want %q", names, want)
	}

	// The two tools with arguments take exactly the documented ones, and
	// every one of them is optional.
	documented := map[string][]string{
		"get_readings":    {"label", "offset", "sensor", "stats", "types"},
		"sample_readings": {"label", "seconds", "sensor", "types", "values"},
	}
	for _, tool := range res.Tools {
		want, ok := documented[tool.Name]
		if !ok {
			continue
		}
		raw, _ := json.Marshal(tool.InputSchema)
		var schema struct {
			Properties map[string]any `json:"properties"`
			Required   []string       `json:"required"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatal(err)
		}
		var props []string
		for name := range schema.Properties {
			props = append(props, name)
		}
		sort.Strings(props)
		if !reflect.DeepEqual(props, want) {
			t.Errorf("%s parameters = %q, want %q", tool.Name, props, want)
		}
		if len(schema.Required) != 0 {
			t.Errorf("%s requires %q; every argument must be optional", tool.Name, schema.Required)
		}
	}

	hello := cs.InitializeResult()
	if hello.ServerInfo.Name != "hwinfo-mcp" || !strings.Contains(hello.Instructions, "thermal_overview") {
		t.Errorf("server info = %+v, instructions = %q", hello.ServerInfo, hello.Instructions)
	}
}

// The MCP bundle's manifest lists the tools for the client to show before the
// server has started. It is written by hand, so it must be kept in step with
// what the server registers.
func TestBundleManifestListsTheServersTools(t *testing.T) {
	cs := connect(t, from(t, fixture()))
	res, err := cs.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var served []string
	for _, tool := range res.Tools {
		served = append(served, tool.Name)
	}
	sort.Strings(served)

	data, err := os.ReadFile(filepath.FromSlash("mcpb/manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	var listed []string
	for _, tool := range manifest.Tools {
		listed = append(listed, tool.Name)
		if tool.Description == "" {
			t.Errorf("mcpb/manifest.json: %s has no description", tool.Name)
		}
	}
	sort.Strings(listed)

	if !reflect.DeepEqual(listed, served) {
		t.Errorf("mcpb/manifest.json lists %q, the server registers %q", listed, served)
	}
}

func TestStatus(t *testing.T) {
	cs := connect(t, from(t, fixture()))
	got, _ := output[statusOutput](t, cs, "hwinfo_status", nil)

	want := statusOutput{
		Available:       true,
		Active:          true,
		LastUpdate:      pollTime.Local().Format(time.RFC3339),
		AgeSeconds:      f(2),
		PollingPeriodMS: 2000,
		Sensors:         5,
		Readings:        19,
		Layout:          "version 2, revision 1",
		Locked:          true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("status:\n got %+v\nwant %+v", got, want)
	}
	if ts, err := time.Parse(time.RFC3339, got.LastUpdate); err != nil || !ts.Equal(pollTime) {
		t.Errorf("last_update = %q, which is not the poll time %v", got.LastUpdate, pollTime)
	}
}

// leftToUser is what a hint says when its fix is a change in HWiNFO's own
// settings: the assistant must not make that change for the user.
const leftToUser = "Leave these steps to the user"

// When there is no data, hwinfo_status still succeeds: explaining what is
// wrong is its job.
func TestStatusExplainsFailures(t *testing.T) {
	tests := []struct {
		err    error
		hint   string
		manual bool // the fix is a step in HWiNFO's settings
	}{
		{fmt.Errorf("%w (%s)", hwinfo.ErrNotFound, hwinfo.MappingName), "Shared Memory Support", true},
		{fmt.Errorf("%w (%s)", hwinfo.ErrAccessDenied, hwinfo.MappingName), "integrity level", false},
		{hwinfo.ErrUnsupported, "Windows", false},
		{fmt.Errorf("%w: signature %q", hwinfo.ErrSignature, "ABCD"), "Update HWiNFO", false},
		{errors.New("something unforeseen"), "", false},
	}
	for _, tt := range tests {
		cs := connect(t, failing(tt.err))
		got, _ := output[statusOutput](t, cs, "hwinfo_status", nil)
		if got.Available || got.Active || got.Locked || got.Sensors != 0 || got.LastUpdate != "" {
			t.Errorf("%v: status claims data: %+v", tt.err, got)
		}
		if got.Problem != tt.err.Error() {
			t.Errorf("%v: problem = %q", tt.err, got.Problem)
		}
		if (tt.hint == "") != (got.Hint == "") || !strings.Contains(got.Hint, tt.hint) {
			t.Errorf("%v: hint = %q, want it to mention %q", tt.err, got.Hint, tt.hint)
		}
		if strings.Contains(got.Hint, leftToUser) != tt.manual {
			t.Errorf("%v: hint = %q; leaving the fix to the user: want %v", tt.err, got.Hint, tt.manual)
		}
		// No hint may send an assistant off to raise anyone's privileges.
		if errors.Is(tt.err, hwinfo.ErrAccessDenied) && !strings.Contains(got.Hint, "do not start the client or HWiNFO with other privileges") {
			t.Errorf("%v: hint = %q, want it to rule out changing privileges", tt.err, got.Hint)
		}
	}
}

// An installation that came with a copy of HWiNFO says where the setup helper
// for it is when HWiNFO is not publishing at all, and at no other time.
func TestAdviceNamesTheSetupHelper(t *testing.T) {
	const helper = `C:\Users\someone\Claude Extensions\hwinfo\hwinfo\Setup-HWiNFO.cmd`
	notFound := fmt.Errorf("%w (%s)", hwinfo.ErrNotFound, hwinfo.MappingName)
	withHelper := func(src source) *mcp.ClientSession {
		return connectTo(t, newServerAt(src, clock, sleepFor, helper))
	}

	cs := withHelper(failing(notFound))
	got, _ := output[statusOutput](t, cs, "hwinfo_status", nil)
	// The helper starts a program, so running it is the user's step too: the
	// hint reaches assistants that have a shell.
	for _, want := range []string{helper, "Shared Memory Support", leftToUser + ", running that file included"} {
		if !strings.Contains(got.Hint, want) {
			t.Errorf("hint = %q, want it to contain %q", got.Hint, want)
		}
	}
	// The tools that need data pass the same advice on.
	res := call(t, cs, "thermal_overview", nil)
	if msg := text(t, res); !res.IsError || !strings.Contains(msg, helper) || !strings.Contains(msg, leftToUser) {
		t.Errorf("thermal_overview: IsError = %v, message = %q; want the helper and who is to run it", res.IsError, msg)
	}

	// Without a copy of HWiNFO there is nothing to point at.
	plain, _ := output[statusOutput](t, connect(t, failing(notFound)), "hwinfo_status", nil)
	if strings.Contains(plain.Hint, "copy of HWiNFO") || strings.Contains(plain.Hint, "Setup-HWiNFO") {
		t.Errorf("hint = %q; this installation has no setup helper to name", plain.Hint)
	}

	// A HWiNFO that is there, but refuses access, publishes something
	// unreadable or has stopped, is not a reason to set another one up.
	stopped := fixture()
	stopped.Dead = true
	for name, src := range map[string]source{
		"access denied": failing(fmt.Errorf("%w (%s)", hwinfo.ErrAccessDenied, hwinfo.MappingName)),
		"bad signature": failing(fmt.Errorf("%w: signature %q", hwinfo.ErrSignature, "ABCD")),
		"inactive":      from(t, stopped),
	} {
		got, _ := output[statusOutput](t, withHelper(src), "hwinfo_status", nil)
		if got.Hint == "" || strings.Contains(got.Hint, "Setup-HWiNFO") {
			t.Errorf("%s: hint = %q, want advice that leaves the setup helper out", name, got.Hint)
		}
	}
}

func TestSetupIn(t *testing.T) {
	root := t.TempDir()
	if got := setupIn(root); got != "" {
		t.Errorf("setupIn found %q in an empty directory", got)
	}

	helper := filepath.Join(root, filepath.FromSlash(setupHelper))
	// A directory of that name is not a helper.
	if err := os.MkdirAll(helper, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := setupIn(root); got != "" {
		t.Errorf("setupIn took the directory %q for the helper", got)
	}
	if err := os.Remove(helper); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(helper, []byte("@echo off\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := setupIn(root); got != helper {
		t.Errorf("setupIn = %q, want %q", got, helper)
	}
}

// The server and the release tool have to agree on where the helper is. The
// tool packs the files of mcpb/hwinfo into the hwinfo folder of the bundle.
func TestSetupHelperIsWhereTheBundlePutsIt(t *testing.T) {
	if _, err := os.Stat(filepath.Join("mcpb", filepath.FromSlash(setupHelper))); err != nil {
		t.Errorf("the server looks for %s in a bundle, but the bundle's sources have none: %v", setupHelper, err)
	}
}

func TestStatusInactive(t *testing.T) {
	im := fixture()
	im.Dead = true
	cs := connect(t, from(t, im))
	got, _ := output[statusOutput](t, cs, "hwinfo_status", nil)

	if !got.Available || got.Active {
		t.Errorf("available = %v, active = %v; want true, false", got.Available, got.Active)
	}
	if got.Readings != 19 || got.LastUpdate == "" {
		t.Errorf("the stale snapshot's details are missing: %+v", got)
	}
	if got.Problem != hwinfo.ErrInactive.Error() || !strings.Contains(got.Hint, "12-hour") {
		t.Errorf("problem = %q, hint = %q", got.Problem, got.Hint)
	}
	// Switching the sensors back on is the user's step, never the assistant's.
	if !strings.Contains(got.Hint, leftToUser) {
		t.Errorf("hint = %q, want it to leave the fix to the user", got.Hint)
	}
}

// HWiNFO can be alive but stuck. Data much older than the polling period is
// flagged even though the block still says it is active.
func TestStatusFlagsOldData(t *testing.T) {
	im := fixture()
	im.PollTime = pollTime.Add(-5 * time.Minute).Unix()
	cs := connect(t, from(t, im))

	status, _ := output[statusOutput](t, cs, "hwinfo_status", nil)
	if !status.Active || *status.AgeSeconds != 302 || !strings.Contains(status.Problem, "5m2s ago") {
		t.Errorf("status = %+v, want active with a 5m2s-old warning", status)
	}
	readings, _ := output[readingsOutput](t, cs, "get_readings", map[string]any{"label": "tctl"})
	if !strings.Contains(readings.Warning, "5m2s ago") || readings.Matched != 1 {
		t.Errorf("get_readings: warning = %q, matched = %d", readings.Warning, readings.Matched)
	}

	// Ten seconds is still fine for a two-second polling period.
	im.PollTime = pollTime.Add(-8 * time.Second).Unix()
	fresh, _ := output[statusOutput](t, connect(t, from(t, im)), "hwinfo_status", nil)
	if fresh.Problem != "" {
		t.Errorf("a 10-second-old snapshot was flagged: %q", fresh.Problem)
	}
}

func TestListSensors(t *testing.T) {
	cs := connect(t, from(t, fixture()))
	got, _ := output[sensorsOutput](t, cs, "list_sensors", nil)

	want := sensorsOutput{
		Active:     true,
		LastUpdate: pollTime.Local().Format(time.RFC3339),
		Sensors: []sensorInfo{
			{Sensor: "CPU [#0]: AMD Ryzen 9 9950X3D", Readings: 1, Types: map[string]int{"usage": 1}},
			{Sensor: "CPU [#0]: AMD Ryzen 9 9950X3D: Enhanced", Readings: 8, Types: map[string]int{"temperature": 6, "power": 1, "other": 1}},
			{Sensor: "dGPU [#0]: NVIDIA GeForce RTX 5080", Readings: 7, Types: map[string]int{"temperature": 3, "fan": 1, "power": 2, "other": 1}},
			{Sensor: "GIGABYTE X870E AORUS ELITE WIFI7 (ITE IT8696E)", Readings: 2, Types: map[string]int{"temperature": 1, "fan": 1}},
			{Sensor: "System: GIGABYTE X870E AORUS ELITE WIFI7", Display: "Sistema: GIGABYTE X870E AORUS ELITE WIFI7", Readings: 1, Types: map[string]int{"other": 1}},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("list_sensors:\n got %+v\nwant %+v", got, want)
	}
}

func TestGetReadings(t *testing.T) {
	cs := connect(t, from(t, fixture()))

	// Everything, without statistics.
	all, _ := output[readingsOutput](t, cs, "get_readings", nil)
	if all.Matched != 19 || len(all.Sensors) != 5 || !all.Active || all.Warning != "" {
		t.Fatalf("unfiltered: matched %d in %d sensors, active %v, warning %q", all.Matched, len(all.Sensors), all.Active, all.Warning)
	}
	for _, s := range all.Sensors {
		for _, r := range s.Readings {
			if r.Min != nil || r.Max != nil || r.Avg != nil {
				t.Fatalf("%s has statistics although stats was not requested", r.Label)
			}
			if r.Type == "" || r.Value == nil {
				t.Fatalf("%s lacks a type or a value: %+v", r.Label, r)
			}
		}
	}

	// A filter in the language HWiNFO displays finds the same reading as
	// its English name does, and values are rounded to three decimals.
	want := readingsOutput{
		Active:     true,
		LastUpdate: pollTime.Local().Format(time.RFC3339),
		Matched:    1,
		Sensors: []sensorReadings{{
			Sensor: "CPU [#0]: AMD Ryzen 9 9950X3D: Enhanced",
			Readings: []reading{{
				Label: "CPU Die (average)", Display: "Caso CPU (m\u00e9dia)", Type: "temperature",
				Value: f(70.661), Unit: degC, Min: f(44), Max: f(78), Avg: f(68),
			}},
		}},
	}
	for _, label := range []string{"caso cpu", "Die (AVERAGE)"} {
		got, _ := output[readingsOutput](t, cs, "get_readings", map[string]any{"label": label, "stats": true})
		if !reflect.DeepEqual(got, want) {
			t.Errorf("label %q:\n got %+v\nwant %+v", label, got, want)
		}
	}

	// Sensor and type filters combine; the unit must be a real degree sign.
	temps, raw := output[readingsOutput](t, cs, "get_readings", map[string]any{"sensor": "rtx 5080", "types": []string{"temperature", "fan"}})
	var labels []string
	for _, s := range temps.Sensors {
		if s.Sensor != "dGPU [#0]: NVIDIA GeForce RTX 5080" {
			t.Errorf("sensor filter let %q through", s.Sensor)
		}
		for _, r := range s.Readings {
			labels = append(labels, r.Label+" "+r.Unit)
		}
	}
	if want := []string{"GPU Temperature " + degC, "GPU Memory Junction Temperature " + degC, "GPU Hot Spot Temperature " + degC, "GPU Fan1 RPM"}; !reflect.DeepEqual(labels, want) {
		t.Errorf("GPU temperatures and fans = %q, want %q", labels, want)
	}
	if !strings.Contains(raw, `"unit":"`+degC+`"`) {
		t.Errorf("the JSON does not carry the degree sign as UTF-8: %s", raw)
	}
}

// A filter that matches nothing is a normal answer, and its empty list must
// be a JSON array: the output schema does not allow null there.
func TestGetReadingsNoMatch(t *testing.T) {
	cs := connect(t, from(t, fixture()))
	got, raw := output[readingsOutput](t, cs, "get_readings", map[string]any{"sensor": "liquid cooler"})
	if got.Matched != 0 || len(got.Sensors) != 0 {
		t.Errorf("got %+v, want no matches", got)
	}
	if !strings.Contains(raw, `"sensors":[]`) {
		t.Errorf("empty result is not an empty array: %s", raw)
	}
}

// One call returns a bounded number of readings, so that the answer always
// fits into a tool result, and says so when it left some out.
func TestGetReadingsIsBounded(t *testing.T) {
	im := hwinfotest.Image{PollTime: pollTime.Unix(), Sensors: []hwinfotest.Sensor{{Name: "Big sensor"}, {Name: "Small sensor"}}}
	for i := range maxReadings + 25 {
		im.Readings = append(im.Readings, hwinfotest.Reading{Type: hwinfotest.Usage, Sensor: 0, Label: fmt.Sprintf("Core %d Usage", i), Unit: "%", Value: float64(i)})
	}
	im.Readings = append(im.Readings, hwinfotest.Reading{Type: hwinfotest.Fan, Sensor: 1, Label: "Fan", Unit: "RPM", Value: 900})
	cs := connect(t, from(t, im))

	count := func(out readingsOutput) (n int) {
		for _, s := range out.Sensors {
			n += len(s.Readings)
		}
		return n
	}

	all, _ := output[readingsOutput](t, cs, "get_readings", nil)
	if all.Matched != maxReadings+26 || !all.Truncated || count(all) != maxReadings {
		t.Errorf("unfiltered: matched %d, truncated %v, returned %d; want %d, true, %d", all.Matched, all.Truncated, count(all), maxReadings+26, maxReadings)
	}
	if len(all.Sensors) != 1 || all.Sensors[0].Readings[maxReadings-1].Label != fmt.Sprintf("Core %d Usage", maxReadings-1) {
		t.Error("the readings returned are not the first ones in HWiNFO's order")
	}

	// A filter brings the rest within reach, and an answer that fits is
	// not flagged.
	fan, raw := output[readingsOutput](t, cs, "get_readings", map[string]any{"types": []string{"fan"}})
	if fan.Matched != 1 || fan.Truncated || count(fan) != 1 || strings.Contains(raw, "truncated") {
		t.Errorf("filtered: %s", raw)
	}

	// So does reading on from where the first answer stopped: everything
	// HWiNFO publishes can be had without a filter.
	if all.NextOffset != maxReadings {
		t.Fatalf("next_offset = %d, want %d", all.NextOffset, maxReadings)
	}
	rest, raw := output[readingsOutput](t, cs, "get_readings", map[string]any{"offset": all.NextOffset})
	if rest.Matched != maxReadings+26 || rest.Truncated || rest.NextOffset != 0 || count(rest) != 26 || strings.Contains(raw, "next_offset") {
		t.Errorf("second page: matched %d, truncated %v, next_offset %d, returned %d", rest.Matched, rest.Truncated, rest.NextOffset, count(rest))
	}
	if len(rest.Sensors) != 2 || rest.Sensors[0].Readings[0].Label != fmt.Sprintf("Core %d Usage", maxReadings) || rest.Sensors[1].Readings[0].Label != "Fan" {
		t.Errorf("the second page does not continue where the first stopped: %s", raw)
	}

	// Past the end there is nothing left, which is not an error.
	if beyond, raw := output[readingsOutput](t, cs, "get_readings", map[string]any{"offset": 100000}); beyond.Matched != maxReadings+26 || count(beyond) != 0 || !strings.Contains(raw, `"sensors":[]`) {
		t.Errorf("past the end: %s", raw)
	}
	if res := call(t, cs, "get_readings", map[string]any{"offset": -1}); !res.IsError || !strings.Contains(text(t, res), "negative") {
		t.Errorf("a negative offset: isError = %v, %s", res.IsError, text(t, res))
	}
}

// ticking is a clock that only moves when the code under test waits, and a
// source of the snapshot HWiNFO would publish at that moment: poll k happens
// k periods after the start.
type ticking struct {
	t      *testing.T
	start  time.Time
	now    time.Time
	period time.Duration
	image  func(poll int) hwinfotest.Image
}

func newTicking(t *testing.T, period time.Duration, image func(poll int) hwinfotest.Image) *ticking {
	return &ticking{t: t, start: pollTime, now: pollTime, period: period, image: image}
}

func (c *ticking) clock() time.Time { return c.now }

func (c *ticking) sleep(ctx context.Context, d time.Duration) error {
	c.now = c.now.Add(d)
	return ctx.Err()
}

func (c *ticking) source() (*hwinfo.Snapshot, error) {
	poll := int(c.now.Sub(c.start) / c.period)
	im := c.image(poll)
	im.PollTime = c.start.Add(time.Duration(poll) * c.period).Unix()
	im.PollingPeriod = uint32(c.period / time.Millisecond)
	snap, err := hwinfo.Parse(im.Bytes())
	if snap == nil {
		c.t.Fatalf("Parse: %v", err)
	}
	snap.Locked = true
	return snap, err
}

// rails is a mainboard whose 12 V rail and CPU power take the given values
// poll after poll, and stay at the last one.
func rails(volts, watts []float64) func(int) hwinfotest.Image {
	at := func(values []float64, poll int) float64 { return values[min(poll, len(values)-1)] }
	return func(poll int) hwinfotest.Image {
		return hwinfotest.Image{
			Sensors: []hwinfotest.Sensor{
				{Name: "GIGABYTE X870E AORUS ELITE WIFI7 (ITE IT8696E)"},
				{Name: "CPU [#0]: AMD Ryzen 9 9950X3D: Enhanced"},
			},
			Readings: []hwinfotest.Reading{
				{Type: hwinfotest.Voltage, Sensor: 0, Label: "+12V", Unit: "V", Value: at(volts, poll), Min: 11.712, Max: 12.288, Avg: 12.05},
				{Type: hwinfotest.Fan, Sensor: 0, Label: "System 2", UserLabel: "Sistema 2", Unit: "RPM", Value: 1310},
				{Type: hwinfotest.Power, Sensor: 1, Label: "CPU Package Power", UserLabel: "Potência total da CPU", Unit: "W", Value: at(watts, poll)},
			},
		}
	}
}

func fs(values ...float64) []*float64 {
	out := make([]*float64, len(values))
	for i := range values {
		out[i] = &values[i]
	}
	return out
}

// One sample per poll, with the moment each extreme was seen: that moment is
// what the tool adds to the minimum and maximum HWiNFO keeps anyway.
func TestSampleReadings(t *testing.T) {
	c := newTicking(t, 2*time.Second, rails(
		[]float64{12.096, 12.192, 11.808, 12, 12.096},
		[]float64{80, 150, 230, 120, 90},
	))
	cs := connectAt(t, c.source, c.clock, c.sleep)
	got, raw := output[sampleOutput](t, cs, "sample_readings", map[string]any{"types": []string{"voltage", "power"}, "seconds": 10, "values": true})

	want := sampleOutput{
		Started:         pollTime.Local().Format(time.RFC3339),
		Seconds:         f(10),
		Samples:         5,
		PollingPeriodMS: 2000,
		SampleTimes:     fs(0, 2, 4, 6, 8),
		Matched:         2,
		Sensors: []sampledSensor{
			{Sensor: "GIGABYTE X870E AORUS ELITE WIFI7 (ITE IT8696E)", Readings: []sampledReading{{
				Label: "+12V", Type: "voltage", Unit: "V",
				First: f(12.096), Last: f(12.096), Min: f(11.808), Max: f(12.192), Mean: f(12.038),
				MinAt: f(4), MaxAt: f(2), Step: f(0.384), StepAt: f(4),
				Values: fs(12.096, 12.192, 11.808, 12, 12.096),
			}}},
			{Sensor: "CPU [#0]: AMD Ryzen 9 9950X3D: Enhanced", Readings: []sampledReading{{
				Label: "CPU Package Power", Display: "Potência total da CPU", Type: "power", Unit: "W",
				First: f(80), Last: f(90), Min: f(80), Max: f(230), Mean: f(134),
				MinAt: f(0), MaxAt: f(4), Step: f(110), StepAt: f(6),
				Values: fs(80, 150, 230, 120, 90),
			}}},
		},
	}
	if !reflect.DeepEqual(got, want) {
		g, _ := json.MarshalIndent(got, "", "  ")
		w, _ := json.MarshalIndent(want, "", "  ")
		t.Errorf("sample_readings:\n got %s\nwant %s", g, w)
	}
	if strings.Contains(raw, "warning") || strings.Contains(raw, "truncated") {
		t.Errorf("a complete window carries a warning: %s", raw)
	}
}

// Without values the answer holds the summary only, and the window defaults
// to ten seconds.
func TestSampleReadingsDefaults(t *testing.T) {
	c := newTicking(t, 2*time.Second, rails([]float64{12, 12.1}, []float64{100}))
	cs := connectAt(t, c.source, c.clock, c.sleep)
	got, raw := output[sampleOutput](t, cs, "sample_readings", map[string]any{"label": "+12V"})

	if *got.Seconds != defaultSampleSeconds || got.Samples != 5 || got.Matched != 1 {
		t.Errorf("seconds %v, samples %d, matched %d; want %d, 5, 1", *got.Seconds, got.Samples, got.Matched, defaultSampleSeconds)
	}
	if strings.Contains(raw, `"values"`) || strings.Contains(raw, "sample_times") {
		t.Errorf("values were returned without being asked for: %s", raw)
	}
	r := got.Sensors[0].Readings[0]
	if *r.Min != 12 || *r.Max != 12.1 || *r.MinAt != 0 || *r.MaxAt != 2 || *r.Step != 0.1 {
		t.Errorf("summary = %s", raw)
	}

	// A reading that never moves has no step to locate.
	steady, raw := output[sampleOutput](t, cs, "sample_readings", map[string]any{"types": []string{"fan"}, "seconds": 4})
	if r := steady.Sensors[0].Readings[0]; *r.Step != 0 || r.StepAt != nil || r.Display != "Sistema 2" {
		t.Errorf("steady reading: %s", raw)
	}
}

func TestSampleReadingsRejectsBadArguments(t *testing.T) {
	c := newTicking(t, 2*time.Second, rails([]float64{12}, []float64{100}))
	cs := connectAt(t, c.source, c.clock, c.sleep)
	for name, args := range map[string]map[string]any{
		"too long":     {"seconds": maxSampleSeconds + 1},
		"negative":     {"seconds": -5},
		"unknown type": {"types": []string{"pressure"}},
	} {
		if res := call(t, cs, "sample_readings", args); !res.IsError {
			t.Errorf("%s: accepted %v", name, args)
		}
	}
	if c.now != c.start {
		t.Error("a rejected call waited before failing")
	}
}

// HWiNFO can stop publishing while it is being watched. What was sampled up
// to then is returned, with the same advice as any stale data.
func TestSampleReadingsWhenHWiNFOStops(t *testing.T) {
	healthy := rails([]float64{12.096, 11.904}, []float64{80, 200})
	c := newTicking(t, 2*time.Second, func(poll int) hwinfotest.Image {
		im := healthy(poll)
		im.Dead = poll >= 2
		return im
	})
	cs := connectAt(t, c.source, c.clock, c.sleep)
	got, raw := output[sampleOutput](t, cs, "sample_readings", map[string]any{"types": []string{"voltage"}, "seconds": 20})

	if got.Samples != 2 || *got.Seconds != 4 {
		t.Errorf("samples %d over %v s, want the 2 taken in the 4 s before HWiNFO stopped", got.Samples, *got.Seconds)
	}
	if !strings.Contains(got.Warning, "stopped after 4s") || !strings.Contains(got.Warning, leftToUser) {
		t.Errorf("warning = %q", got.Warning)
	}
	if r := got.Sensors[0].Readings[0]; *r.First != 12.096 || *r.Last != 11.904 {
		t.Errorf("the samples taken before the stop are missing: %s", raw)
	}

	// Stopped from the start, there is nothing to watch at all.
	dead := newTicking(t, 2*time.Second, func(poll int) hwinfotest.Image {
		im := healthy(poll)
		im.Dead = true
		return im
	})
	res := call(t, connectAt(t, dead.source, dead.clock, dead.sleep), "sample_readings", nil)
	if !res.IsError || !strings.Contains(text(t, res), "nothing to sample") || !strings.Contains(text(t, res), leftToUser) {
		t.Errorf("inactive from the start: isError = %v, %s", res.IsError, text(t, res))
	}
}

// HWiNFO adds and removes sensors while it runs, which moves the readings
// that follow. The window ends there rather than mixing two readings up.
func TestSampleReadingsWhenTheSensorListChanges(t *testing.T) {
	steady := rails([]float64{12.096, 12.192, 12.288}, []float64{80})
	c := newTicking(t, 2*time.Second, func(poll int) hwinfotest.Image {
		im := steady(poll)
		if poll >= 2 {
			// A sensor appears in front of the others, as PresentMon's does
			// when a game starts.
			im.Sensors = append([]hwinfotest.Sensor{{Name: "PresentMon [game.exe]"}}, im.Sensors...)
			for i := range im.Readings {
				im.Readings[i].Sensor++
			}
			im.Readings = append([]hwinfotest.Reading{{Type: hwinfotest.Voltage, Sensor: 0, Label: "Not a voltage rail", Unit: "V", Value: 99}}, im.Readings...)
		}
		return im
	})
	cs := connectAt(t, c.source, c.clock, c.sleep)
	got, raw := output[sampleOutput](t, cs, "sample_readings", map[string]any{"label": "+12V", "seconds": 20})

	if got.Samples != 2 || !strings.Contains(got.Warning, "list of sensors changed") {
		t.Errorf("samples %d, warning %q", got.Samples, got.Warning)
	}
	if r := got.Sensors[0].Readings[0]; *r.Max != 12.192 {
		t.Errorf("a reading from the new layout was mixed in: %s", raw)
	}
}

// The answer stays bounded however much matches, and every sampled value is
// only spelled out for a handful of readings.
func TestSampleReadingsIsBounded(t *testing.T) {
	c := newTicking(t, 2*time.Second, func(int) hwinfotest.Image {
		im := hwinfotest.Image{Sensors: []hwinfotest.Sensor{{Name: "Big sensor"}}}
		for i := range maxSampled + 5 {
			im.Readings = append(im.Readings, hwinfotest.Reading{Type: hwinfotest.Usage, Sensor: 0, Label: fmt.Sprintf("Core %d Usage", i), Unit: "%", Value: float64(i)})
		}
		return im
	})
	cs := connectAt(t, c.source, c.clock, c.sleep)
	got, raw := output[sampleOutput](t, cs, "sample_readings", map[string]any{"seconds": 2, "values": true})

	if got.Matched != maxSampled+5 || !got.Truncated || len(got.Sensors[0].Readings) != maxSampled {
		t.Errorf("matched %d, truncated %v, watched %d", got.Matched, got.Truncated, len(got.Sensors[0].Readings))
	}
	if strings.Contains(raw, `"values"`) || !strings.Contains(got.Warning, "narrow the filters") {
		t.Errorf("values for %d readings were returned, or their absence is unexplained: %q", maxSampled, got.Warning)
	}
}

// With a polling period under a second the poll time cannot tell two polls
// apart, so the period paces the samples.
func TestSampleReadingsWithFastPolling(t *testing.T) {
	c := newTicking(t, 250*time.Millisecond, rails([]float64{12, 12.1, 12.2, 12.3, 12.4}, []float64{100}))
	cs := connectAt(t, c.source, c.clock, c.sleep)
	got, raw := output[sampleOutput](t, cs, "sample_readings", map[string]any{"label": "+12V", "seconds": 1, "values": true})

	if got.Samples != 4 || !reflect.DeepEqual(got.SampleTimes, fs(0, 0.3, 0.6, 0.9)) {
		t.Errorf("samples at %s, want four of them about a polling period apart", raw)
	}
}

// A window that is cancelled ends at once instead of running to its end.
func TestSampleReadingsHonoursCancellation(t *testing.T) {
	c := newTicking(t, 2*time.Second, rails([]float64{12}, []float64{100}))
	h := &handlers{src: c.source, now: c.clock, sleep: sleepFor}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	begun := time.Now()
	_, _, err := h.sampleReadings(ctx, nil, sampleInput{Seconds: maxSampleSeconds})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want the cancellation", err)
	}
	if waited := time.Since(begun); waited > 5*time.Second {
		t.Errorf("a cancelled window still took %v", waited)
	}
}

// Watching must not hold up the rest: the other tools answer while a window
// is open.
func TestOtherToolsAnswerWhileSampling(t *testing.T) {
	c := newTicking(t, 2*time.Second, rails([]float64{12}, []float64{100}))

	// Two calls now read the clock at once, so it is guarded; the window's
	// first wait blocks until the test lets it go.
	var mu sync.Mutex
	clock := func() time.Time { mu.Lock(); defer mu.Unlock(); return c.now }
	src := func() (*hwinfo.Snapshot, error) { mu.Lock(); defer mu.Unlock(); return c.source() }
	waiting, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	sleep := func(ctx context.Context, d time.Duration) error {
		once.Do(func() { close(waiting) })
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
		mu.Lock()
		defer mu.Unlock()
		c.now = c.now.Add(d)
		return nil
	}
	cs := connectAt(t, src, clock, sleep)

	sampled := make(chan *mcp.CallToolResult, 1)
	go func() {
		res, err := cs.CallTool(t.Context(), &mcp.CallToolParams{Name: "sample_readings", Arguments: map[string]any{"seconds": 2}})
		if err != nil {
			t.Errorf("sample_readings: %v", err)
		}
		sampled <- res
	}()

	select {
	case <-waiting:
	case <-time.After(10 * time.Second):
		t.Fatal("the sampling window never started waiting")
	}
	answered := make(chan struct{})
	go func() {
		defer close(answered)
		if res := call(t, cs, "hwinfo_status", nil); res.IsError {
			t.Errorf("hwinfo_status failed during a window: %s", text(t, res))
		}
	}()
	select {
	case <-answered:
	case <-time.After(10 * time.Second):
		t.Fatal("hwinfo_status did not answer while a sampling window was open")
	}

	close(release)
	select {
	case res := <-sampled:
		if res == nil || res.IsError {
			t.Errorf("the window did not complete: %+v", res)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the sampling window did not end after it was released")
	}
}

// A source that yields nothing at all is reported like any other failure.
func TestToolsWithoutAnySnapshot(t *testing.T) {
	cs := connect(t, func() (*hwinfo.Snapshot, error) { return nil, nil })
	for _, tool := range []string{"list_sensors", "get_readings", "thermal_overview"} {
		if res := call(t, cs, tool, nil); !res.IsError || !strings.Contains(text(t, res), "no sensor data") {
			t.Errorf("%s: isError = %v, %s", tool, res.IsError, text(t, res))
		}
	}
	if status, _ := output[statusOutput](t, cs, "hwinfo_status", nil); status.Available || status.Problem != "no sensor data" {
		t.Errorf("status = %+v", status)
	}
}

func TestGetReadingsRejectsUnknownType(t *testing.T) {
	cs := connect(t, from(t, fixture()))
	res := call(t, cs, "get_readings", map[string]any{"types": []string{"temperature", "pressure"}})
	if !res.IsError {
		t.Fatal("an unknown reading type was accepted")
	}
	if msg := text(t, res); !strings.Contains(msg, `"pressure"`) || !strings.Contains(msg, "temperature, voltage, fan, current, power, clock, usage, other") {
		t.Errorf("error does not name the bad type and the valid ones: %s", msg)
	}
}

// JSON has no NaN or infinity. A reading that holds one is reported with a
// null value instead of failing the whole call.
func TestGetReadingsNonFiniteValues(t *testing.T) {
	im := fixture()
	im.Readings[1].Value = math.NaN()
	im.Readings[1].Max = math.Inf(1)
	im.Readings[1].Min = math.Copysign(0, -1)
	cs := connect(t, from(t, im))

	got, raw := output[readingsOutput](t, cs, "get_readings", map[string]any{"label": "Tctl", "stats": true})
	if got.Matched != 1 {
		t.Fatalf("matched %d, want 1", got.Matched)
	}
	r := got.Sensors[0].Readings[0]
	if r.Value != nil || r.Max != nil {
		t.Errorf("NaN and +Inf were not dropped: %+v", r)
	}
	if r.Min == nil || math.Signbit(*r.Min) || r.Avg == nil || *r.Avg != 70 {
		t.Errorf("finite statistics were lost or mangled: min %v, avg %v", r.Min, r.Avg)
	}
	if !strings.Contains(raw, `"value":null`) || strings.Contains(raw, `"min":-0`) {
		t.Errorf("unexpected encoding: %s", raw)
	}
}

func TestThermalOverview(t *testing.T) {
	cs := connect(t, from(t, fixture()))
	got, _ := output[thermalOutput](t, cs, "thermal_overview", nil)

	want := thermalOutput{
		Active:     true,
		LastUpdate: pollTime.Local().Format(time.RFC3339),
		CPUs: []cpuThermals{{
			Name:        "AMD Ryzen 9 9950X3D",
			Temperature: &reading{Label: "CPU (Tctl/Tdie)", Value: f(73), Unit: degC, Min: f(45.5), Max: f(80.125), Avg: f(70)},
			DieAverage:  &reading{Label: "CPU Die (average)", Display: "Caso CPU (m\u00e9dia)", Value: f(70.661), Unit: degC, Min: f(44), Max: f(78), Avg: f(68)},
			CCDs: []reading{
				{Label: "CPU CCD1 (Tdie)", Value: f(65.25), Unit: degC, Min: f(40), Max: f(80.125), Avg: f(60)},
				{Label: "CPU CCD2 (Tdie)", Value: f(70.5), Unit: degC, Min: f(41), Max: f(77.25), Avg: f(62)},
			},
			HottestCore:  &reading{Label: "Core10 (CCD2)", Value: f(69.219), Unit: degC, Min: f(41), Max: f(73), Avg: f(57)},
			PackagePower: &reading{Label: "CPU Package Power", Display: "Pot\u00eancia total da CPU", Value: f(166.3), Unit: "W", Min: f(38), Max: f(190), Avg: f(120)},
			Throttling:   &throttling{},
		}},
		GPUs: []gpuThermals{{
			Name:           "NVIDIA GeForce RTX 5080",
			Temperature:    &reading{Label: "GPU Temperature", Display: "Temperatura GPU", Value: f(59.355), Unit: degC, Min: f(40), Max: f(66), Avg: f(55)},
			HotSpot:        &reading{Label: "GPU Hot Spot Temperature", Display: "Temperatura de ponto quente da GPU", Value: f(70.594), Unit: degC, Min: f(50), Max: f(83), Avg: f(66)},
			MemoryJunction: &reading{Label: "GPU Memory Junction Temperature", Display: "Temperatura de jun\u00e7\u00e3o da mem\u00f3ria GPU", Value: f(66), Unit: degC, Min: f(44), Max: f(70), Avg: f(60)},
			Power:          &reading{Label: "GPU Power", Display: "GPU Pot\u00eancia", Value: f(220.153), Unit: "W", Min: f(20), Max: f(310), Avg: f(150)},
			Fans:           []reading{{Label: "GPU Fan1", Display: "GPU Ventilador1", Value: f(1330), Unit: "RPM", Min: f(0), Max: f(1800), Avg: f(900)}},
			Throttling:     &throttling{Occurred: true},
		}},
		Other: []sensorReadings{{
			Sensor:   "GIGABYTE X870E AORUS ELITE WIFI7 (ITE IT8696E)",
			Readings: []reading{{Label: "CPU", Value: f(73), Unit: degC, Min: f(45), Max: f(80), Avg: f(70)}},
		}},
		Fans: []sensorReadings{{
			Sensor:   "GIGABYTE X870E AORUS ELITE WIFI7 (ITE IT8696E)",
			Readings: []reading{{Label: "System 2", Display: "Sistema 2", Value: f(1310), Unit: "RPM", Min: f(800), Max: f(1500), Avg: f(1100)}},
		}},
	}
	if !reflect.DeepEqual(got, want) {
		g, _ := json.MarshalIndent(got, "", "  ")
		w, _ := json.MarshalIndent(want, "", "  ")
		t.Errorf("thermal_overview:\n got %s\nwant %s", g, w)
	}
}

// A CPU that was held at its temperature limit shows it in thermal_limit even
// though no throttling flag was ever set. Reporting only the flags made such
// a CPU look as if temperature had never limited it.
func TestThermalOverviewReportsTheThermalLimit(t *testing.T) {
	im := fixture()
	im.Readings = append(im.Readings, hwinfotest.Reading{
		Type: hwinfotest.Usage, Sensor: 1, Label: "Thermal Limit", Unit: "%", Value: 76.842, Min: 47.895, Max: 100.016, Avg: 73.685,
	})
	cs := connect(t, from(t, im))
	got, raw := output[thermalOutput](t, cs, "thermal_overview", nil)

	cpu := got.CPUs[0]
	want := &reading{Label: "Thermal Limit", Value: f(76.842), Unit: "%", Min: f(47.895), Max: f(100.016), Avg: f(73.685)}
	if !reflect.DeepEqual(cpu.ThermalLimit, want) {
		t.Errorf("thermal limit = %+v, want %+v", cpu.ThermalLimit, want)
	}
	if cpu.Throttling == nil || cpu.Throttling.Active || cpu.Throttling.Occurred {
		t.Errorf("throttling = %+v, want the flags reported as they are: clear", cpu.Throttling)
	}

	// The names on the wire are part of what clients rely on.
	var wire struct {
		CPUs []map[string]json.RawMessage `json:"cpus"`
		GPUs []map[string]json.RawMessage `json:"gpus"`
	}
	if err := json.Unmarshal([]byte(raw), &wire); err != nil || len(wire.CPUs) != 1 || len(wire.GPUs) != 1 {
		t.Fatalf("cannot read the devices back (%v): %s", err, raw)
	}
	for _, name := range []string{"thermal_limit", "thermal_throttling", "package_power", "temperature"} {
		if _, ok := wire.CPUs[0][name]; !ok {
			t.Errorf("the CPU has no %q field: %s", name, raw)
		}
	}
	if _, ok := wire.GPUs[0]["power"]; !ok {
		t.Errorf("the GPU has no \"power\" field: %s", raw)
	}
	if _, ok := wire.GPUs[0]["board_power"]; ok {
		t.Errorf("the GPU's power is still called board_power: %s", raw)
	}
}

// With no CPU, GPU or temperature sensor at all, every list is an empty
// array, which is what the output schema promises.
func TestThermalOverviewOnABareSystem(t *testing.T) {
	im := hwinfotest.Image{
		PollTime: pollTime.Unix(),
		Sensors:  []hwinfotest.Sensor{{Name: "Network: Some Adapter"}},
		Readings: []hwinfotest.Reading{{Type: hwinfotest.Other, Sensor: 0, Label: "Total DL", Unit: "MB"}},
	}
	for _, src := range []source{from(t, im), from(t, hwinfotest.Image{PollTime: pollTime.Unix()})} {
		cs := connect(t, src)
		_, raw := output[thermalOutput](t, cs, "thermal_overview", nil)
		for _, field := range []string{`"cpus":[]`, `"gpus":[]`, `"other_temperatures":[]`, `"fans":[]`} {
			if !strings.Contains(raw, field) {
				t.Errorf("missing %s in %s", field, raw)
			}
		}
		if _, raw := output[sensorsOutput](t, cs, "list_sensors", nil); !strings.Contains(raw, `"sensors":[`) {
			t.Errorf("list_sensors: %s", raw)
		}
	}
}

// Stale data is still returned, clearly marked, by every tool that reads it.
func TestDataToolsOnInactiveHWiNFO(t *testing.T) {
	im := fixture()
	im.Dead = true
	cs := connect(t, from(t, im))

	readings, _ := output[readingsOutput](t, cs, "get_readings", map[string]any{"label": "GPU Power"})
	sensors, _ := output[sensorsOutput](t, cs, "list_sensors", nil)
	thermal, _ := output[thermalOutput](t, cs, "thermal_overview", nil)

	for tool, got := range map[string]struct {
		active  bool
		warning string
	}{
		"get_readings":     {readings.Active, readings.Warning},
		"list_sensors":     {sensors.Active, sensors.Warning},
		"thermal_overview": {thermal.Active, thermal.Warning},
	} {
		if got.active || !strings.Contains(got.warning, "stale") || !strings.Contains(got.warning, leftToUser) {
			t.Errorf("%s: active = %v, warning = %q", tool, got.active, got.warning)
		}
	}
	if readings.Matched != 2 || len(sensors.Sensors) != 5 || len(thermal.GPUs) != 1 {
		t.Error("the stale data itself is missing")
	}
}

// Without any data, the tools that need it fail with advice the model can
// pass on, as a tool error rather than a protocol error.
func TestDataToolsFailWithAdvice(t *testing.T) {
	cs := connect(t, failing(fmt.Errorf("%w (%s)", hwinfo.ErrNotFound, hwinfo.MappingName)))
	for _, tool := range []string{"list_sensors", "get_readings", "thermal_overview"} {
		res := call(t, cs, tool, nil)
		if !res.IsError {
			t.Errorf("%s succeeded without data", tool)
			continue
		}
		msg := text(t, res)
		if !strings.Contains(msg, "shared memory not found") || !strings.Contains(msg, "Shared Memory Support") {
			t.Errorf("%s: error lacks the cause or the fix: %s", tool, msg)
		}
		if !strings.Contains(msg, leftToUser) {
			t.Errorf("%s: error does not leave the fix to the user: %s", tool, msg)
		}
	}
}

func TestStaleAfter(t *testing.T) {
	for period, want := range map[time.Duration]time.Duration{
		0:                      10 * time.Second, // unknown period
		500 * time.Millisecond: 10 * time.Second,
		2 * time.Second:        10 * time.Second,
		10 * time.Second:       50 * time.Second,
	} {
		if got := staleAfter(period); got != want {
			t.Errorf("staleAfter(%v) = %v, want %v", period, got, want)
		}
	}
}
