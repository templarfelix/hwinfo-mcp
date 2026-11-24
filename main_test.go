package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
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
	serverSide, clientSide := mcp.NewInMemoryTransports()
	ss, err := newServerAt(src, clock).Connect(t.Context(), serverSide, nil)
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
	if want := []string{"get_readings", "hwinfo_status", "list_sensors", "thermal_overview"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("tools = %q, want %q", names, want)
	}

	// get_readings takes exactly the four documented filters, all optional.
	for _, tool := range res.Tools {
		if tool.Name != "get_readings" {
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
		if want := []string{"label", "sensor", "stats", "types"}; !reflect.DeepEqual(props, want) {
			t.Errorf("get_readings parameters = %q, want %q", props, want)
		}
		if len(schema.Required) != 0 {
			t.Errorf("get_readings requires %q; every filter must be optional", schema.Required)
		}
	}

	hello := cs.InitializeResult()
	if hello.ServerInfo.Name != "hwinfo-mcp" || !strings.Contains(hello.Instructions, "thermal_overview") {
		t.Errorf("server info = %+v, instructions = %q", hello.ServerInfo, hello.Instructions)
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
			BoardPower:     &reading{Label: "GPU Power", Display: "GPU Pot\u00eancia", Value: f(220.153), Unit: "W", Min: f(20), Max: f(310), Avg: f(150)},
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
