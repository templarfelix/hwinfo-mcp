// Command hwinfo-mcp is a read-only Model Context Protocol server for the
// sensor data HWiNFO publishes through its shared memory interface.
//
// It speaks MCP over stdin/stdout and offers four tools:
//
//	hwinfo_status     whether HWiNFO is publishing, and how fresh the data is
//	list_sensors      the sensors HWiNFO knows and what each one measures
//	get_readings      readings, filtered by sensor, label and type
//	thermal_overview  the key CPU and GPU temperatures and power draws
//
// HWiNFO must be running with the Sensors window open and "Shared Memory
// Support" enabled in its settings.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/templarfelix/hwinfo-mcp/internal/hwinfo"
)

// version is set with -ldflags "-X main.version=v1.2.3" in release builds.
var version = "dev"

func buildVersion() string {
	if version != "dev" && version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return "dev"
}

const instructions = `Read-only access to the hardware sensors that HWiNFO monitors on this Windows PC: temperatures, voltages, fan speeds, power draw, clocks and usage.

Start with thermal_overview for the key CPU and GPU temperatures. For anything else, call list_sensors to see what exists, then get_readings with a sensor, label or types filter: unfiltered, it returns hundreds of readings.

Labels are HWiNFO's original English ones; when HWiNFO runs in another language or a sensor was renamed, the displayed name is given alongside. If a tool reports that sensor data is unavailable, hwinfo_status explains why and how to fix it.`

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "hwinfo-mcp serves HWiNFO sensor readings over the Model Context Protocol on stdin/stdout.")
		fmt.Fprintln(flag.CommandLine.Output(), "\nUsage: hwinfo-mcp [-version]")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() > 0 {
		flag.Usage()
		os.Exit(2)
	}
	if *showVersion {
		fmt.Println("hwinfo-mcp", buildVersion())
		return
	}

	// stdout carries the protocol, so diagnostics go to stderr only.
	log.SetFlags(0)
	log.SetPrefix("hwinfo-mcp: ")

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	server := newServer(hwinfo.Reader{}.Snapshot)
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}

// A source returns the current state of HWiNFO's sensors. Like
// hwinfo.Reader.Snapshot, it may return a stale snapshot together with
// hwinfo.ErrInactive.
type source func() (*hwinfo.Snapshot, error)

type handlers struct {
	src source
	now func() time.Time
}

// newServer builds the MCP server around src.
func newServer(src source) *mcp.Server {
	return newServerAt(src, time.Now)
}

// newServerAt is newServer with a replaceable clock, which is what decides
// whether a snapshot counts as fresh.
func newServerAt(src source, now func() time.Time) *mcp.Server {
	h := &handlers{src: src, now: now}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "hwinfo-mcp",
		Title:   "HWiNFO sensors",
		Version: buildVersion(),
	}, &mcp.ServerOptions{
		Instructions: instructions,
		// The tool list never changes and nothing is logged to the client.
		Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}},
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "hwinfo_status",
		Description: "Report whether HWiNFO is publishing sensor data, when it last updated, and how many sensors and readings it exposes. When data is unavailable or stale, explains the likely cause and the fix.",
		Annotations: readOnly("HWiNFO status"),
	}, h.status)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_sensors",
		Description: "List the sensors HWiNFO monitors (CPU, GPU, mainboard, memory, drives, network) with the number of readings of each type. Use it to find the sensor names to pass to get_readings.",
		Annotations: readOnly("List HWiNFO sensors"),
	}, h.listSensors)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_readings",
		Description: "Return current sensor readings from HWiNFO, grouped by sensor. Narrow the result with the sensor, label and types filters; without filters every reading is returned, which is several hundred on a typical PC. Set stats to add the minimum, maximum and average HWiNFO has recorded since it started.",
		Annotations: readOnly("Get HWiNFO readings"),
	}, h.getReadings)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "thermal_overview",
		Description: "Summarise how hot the PC runs: for each CPU the control temperature (Tctl/Tdie or package), die average, per-CCD temperatures, hottest core and package power; for each GPU the core, hot spot and memory junction temperatures, board power and fans; plus the remaining temperatures (mainboard, memory, drives) and fan speeds. Every reading includes its minimum, maximum and average since HWiNFO started.",
		Annotations: readOnly("Thermal overview"),
	}, h.thermalOverview)

	return server
}

func readOnly(title string) *mcp.ToolAnnotations {
	closedWorld := false
	return &mcp.ToolAnnotations{
		Title:          title,
		ReadOnlyHint:   true,
		IdempotentHint: true,
		OpenWorldHint:  &closedWorld,
	}
}

// Tool inputs and outputs. The json tags name the fields on the wire and the
// jsonschema tags become their descriptions in the tool schemas.

type noInput struct{}

type readingsInput struct {
	Sensor string   `json:"sensor,omitempty" jsonschema:"case-insensitive part of a sensor name, such as CPU or RTX 5080; matches the original and the displayed name"`
	Label  string   `json:"label,omitempty" jsonschema:"case-insensitive part of a reading label, such as Temperature or Tctl; matches the original and the displayed label"`
	Types  []string `json:"types,omitempty" jsonschema:"reading types to include, any of: temperature, voltage, fan, current, power, clock, usage, other"`
	Stats  bool     `json:"stats,omitempty" jsonschema:"also return the minimum, maximum and average recorded since HWiNFO started"`
}

type statusOutput struct {
	Available       bool     `json:"available" jsonschema:"whether HWiNFO's shared memory could be opened"`
	Active          bool     `json:"active" jsonschema:"whether HWiNFO is updating the sensor data right now"`
	LastUpdate      string   `json:"last_update,omitempty" jsonschema:"time of HWiNFO's last sensor poll, RFC 3339"`
	AgeSeconds      *float64 `json:"age_seconds,omitempty" jsonschema:"seconds elapsed since the last poll"`
	PollingPeriodMS int64    `json:"polling_period_ms,omitempty" jsonschema:"HWiNFO's sensor polling interval"`
	Sensors         int      `json:"sensors" jsonschema:"number of sensors"`
	Readings        int      `json:"readings" jsonschema:"number of readings"`
	Layout          string   `json:"layout,omitempty" jsonschema:"version and revision of the shared memory layout"`
	Locked          bool     `json:"locked" jsonschema:"whether the snapshot was taken while holding HWiNFO's mutex, which rules out a half-updated copy"`
	Problem         string   `json:"problem,omitempty" jsonschema:"why sensor data is unavailable or stale"`
	Hint            string   `json:"hint,omitempty" jsonschema:"how to fix the problem"`
}

type sensorInfo struct {
	Sensor   string         `json:"sensor" jsonschema:"HWiNFO's original English sensor name"`
	Display  string         `json:"display,omitempty" jsonschema:"name shown in HWiNFO when it was translated or renamed"`
	Readings int            `json:"readings" jsonschema:"number of readings the sensor has"`
	Types    map[string]int `json:"types" jsonschema:"number of readings per reading type"`
}

type sensorsOutput struct {
	Active     bool         `json:"active" jsonschema:"whether HWiNFO is updating the sensor data right now"`
	LastUpdate string       `json:"last_update,omitempty" jsonschema:"time of HWiNFO's last sensor poll, RFC 3339"`
	Warning    string       `json:"warning,omitempty" jsonschema:"set when the data is stale"`
	Sensors    []sensorInfo `json:"sensors"`
}

type reading struct {
	Label   string   `json:"label" jsonschema:"HWiNFO's original English label"`
	Display string   `json:"display,omitempty" jsonschema:"label shown in HWiNFO when it was translated or renamed"`
	Type    string   `json:"type,omitempty" jsonschema:"temperature, voltage, fan, current, power, clock, usage or other"`
	Value   *float64 `json:"value" jsonschema:"current value; null if HWiNFO holds no finite number"`
	Unit    string   `json:"unit,omitempty"`
	Min     *float64 `json:"min,omitempty" jsonschema:"lowest value since HWiNFO started"`
	Max     *float64 `json:"max,omitempty" jsonschema:"highest value since HWiNFO started"`
	Avg     *float64 `json:"avg,omitempty" jsonschema:"average since HWiNFO started"`
}

type sensorReadings struct {
	Sensor   string    `json:"sensor" jsonschema:"HWiNFO's original English sensor name"`
	Display  string    `json:"display,omitempty" jsonschema:"name shown in HWiNFO when it was translated or renamed"`
	Readings []reading `json:"readings"`
}

type readingsOutput struct {
	Active     bool             `json:"active" jsonschema:"whether HWiNFO is updating the sensor data right now"`
	LastUpdate string           `json:"last_update,omitempty" jsonschema:"time of HWiNFO's last sensor poll, RFC 3339"`
	Warning    string           `json:"warning,omitempty" jsonschema:"set when the data is stale"`
	Matched    int              `json:"matched" jsonschema:"number of readings returned"`
	Sensors    []sensorReadings `json:"sensors"`
}

type throttling struct {
	Active   bool `json:"active" jsonschema:"a thermal limit is in effect right now"`
	Occurred bool `json:"occurred" jsonschema:"a thermal limit was hit at some point since HWiNFO started"`
}

type cpuThermals struct {
	Name         string      `json:"name"`
	Temperature  *reading    `json:"temperature,omitempty" jsonschema:"the temperature the CPU regulates itself by: Tctl/Tdie on AMD, package on Intel"`
	DieAverage   *reading    `json:"die_average,omitempty"`
	CCDs         []reading   `json:"ccds,omitempty" jsonschema:"temperature of each core complex die (AMD)"`
	HottestCore  *reading    `json:"hottest_core,omitempty" jsonschema:"the core that is hottest right now"`
	PackagePower *reading    `json:"package_power,omitempty"`
	Throttling   *throttling `json:"thermal_throttling,omitempty"`
}

type gpuThermals struct {
	Name           string      `json:"name"`
	Temperature    *reading    `json:"temperature,omitempty" jsonschema:"GPU core temperature"`
	HotSpot        *reading    `json:"hot_spot,omitempty" jsonschema:"hottest point of the GPU die"`
	MemoryJunction *reading    `json:"memory_junction,omitempty" jsonschema:"hottest video memory chip"`
	BoardPower     *reading    `json:"board_power,omitempty" jsonschema:"power drawn by the whole graphics card"`
	Fans           []reading   `json:"fans,omitempty"`
	Throttling     *throttling `json:"thermal_throttling,omitempty"`
}

type thermalOutput struct {
	Active     bool             `json:"active" jsonschema:"whether HWiNFO is updating the sensor data right now"`
	LastUpdate string           `json:"last_update,omitempty" jsonschema:"time of HWiNFO's last sensor poll, RFC 3339"`
	Warning    string           `json:"warning,omitempty" jsonschema:"set when the data is stale"`
	CPUs       []cpuThermals    `json:"cpus"`
	GPUs       []gpuThermals    `json:"gpus"`
	Other      []sensorReadings `json:"other_temperatures" jsonschema:"temperatures of the remaining sensors: mainboard, memory, drives"`
	Fans       []sensorReadings `json:"fans" jsonschema:"fan speeds other than the graphics card's"`
}

// hint says how to fix the condition err describes, or "" if there is no
// specific advice.
func hint(err error) string {
	switch {
	case errors.Is(err, hwinfo.ErrNotFound):
		return `HWiNFO is not publishing sensor data. Make sure HWiNFO is running with the Sensors window open and "Shared Memory Support" enabled in its settings. If the option was switched on while HWiNFO was already running, restart HWiNFO. The free edition switches it off again after 12 hours.`
	case errors.Is(err, hwinfo.ErrInactive):
		return `HWiNFO stopped updating its shared memory: the Sensors window was closed or the free edition's 12-hour limit was reached. Reopen the Sensors window, re-enable "Shared Memory Support", or restart HWiNFO.`
	case errors.Is(err, hwinfo.ErrAccessDenied):
		return "HWiNFO and this server run at different integrity levels. Start the MCP client with the same privileges as HWiNFO."
	case errors.Is(err, hwinfo.ErrUnsupported):
		return "HWiNFO only exists for Windows; run this server on the Windows PC that HWiNFO monitors."
	case errors.Is(err, hwinfo.ErrSignature), errors.Is(err, hwinfo.ErrLayout):
		return "The shared memory does not look like HWiNFO's sensor block. Update HWiNFO, or report the layout to the hwinfo-mcp project."
	}
	return ""
}

// staleAfter is how old the last poll may be before the data counts as stale.
func staleAfter(period time.Duration) time.Duration {
	return max(10*time.Second, 5*period)
}

// freshness describes how current a snapshot is. warning is empty when
// HWiNFO is active and has polled recently.
func (h *handlers) freshness(snap *hwinfo.Snapshot) (lastUpdate string, age *float64, warning string) {
	if !snap.PollTime.IsZero() {
		lastUpdate = snap.PollTime.Local().Format(time.RFC3339)
		// The poll time has one-second resolution, so a poll that just
		// happened can appear to lie slightly in the future.
		elapsed := max(h.now().Sub(snap.PollTime), 0)
		age = num(elapsed.Seconds())
		if snap.Active && elapsed > staleAfter(snap.PollingPeriod) {
			warning = fmt.Sprintf("HWiNFO last updated its sensors %s ago, so these values may be out of date. It may be frozen or busy.", elapsed.Round(time.Second))
		}
	}
	if !snap.Active {
		warning = "These values are stale. " + hint(hwinfo.ErrInactive)
	}
	return lastUpdate, age, warning
}

// load fetches a snapshot for the tools that return sensor data. A stale
// snapshot is passed on (freshness flags it); every other failure becomes an
// error that says what to do about it.
func (h *handlers) load() (*hwinfo.Snapshot, error) {
	snap, err := h.src()
	if err == nil || (snap != nil && errors.Is(err, hwinfo.ErrInactive)) {
		return snap, nil
	}
	if advice := hint(err); advice != "" {
		return nil, fmt.Errorf("%w. %s", err, advice)
	}
	return nil, err
}

func (h *handlers) status(_ context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, statusOutput, error) {
	snap, err := h.src()
	if snap == nil {
		if err == nil {
			err = errors.New("no sensor data")
		}
		return nil, statusOutput{Problem: err.Error(), Hint: hint(err)}, nil
	}

	out := statusOutput{
		Available:       true,
		Active:          snap.Active,
		PollingPeriodMS: snap.PollingPeriod.Milliseconds(),
		Sensors:         len(snap.Sensors),
		Readings:        len(snap.Readings),
		Layout:          fmt.Sprintf("version %d, revision %d", snap.Version, snap.Revision),
		Locked:          snap.Locked,
	}
	var warning string
	out.LastUpdate, out.AgeSeconds, warning = h.freshness(snap)
	switch {
	case err != nil:
		out.Problem, out.Hint = err.Error(), hint(err)
	case warning != "":
		out.Problem = warning
	}
	return nil, out, nil
}

func (h *handlers) listSensors(_ context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, sensorsOutput, error) {
	snap, err := h.load()
	if err != nil {
		return nil, sensorsOutput{}, err
	}
	out := sensorsOutput{Active: snap.Active, Sensors: make([]sensorInfo, len(snap.Sensors))}
	out.LastUpdate, _, out.Warning = h.freshness(snap)

	for i, s := range snap.Sensors {
		out.Sensors[i] = sensorInfo{
			Sensor:  s.OriginalName,
			Display: displayName(s.Name, s.OriginalName),
			Types:   map[string]int{},
		}
	}
	for _, r := range snap.Readings {
		if r.SensorIndex < 0 {
			continue
		}
		out.Sensors[r.SensorIndex].Readings++
		out.Sensors[r.SensorIndex].Types[r.Type.String()]++
	}
	return nil, out, nil
}

func (h *handlers) getReadings(_ context.Context, _ *mcp.CallToolRequest, in readingsInput) (*mcp.CallToolResult, readingsOutput, error) {
	filter := hwinfo.Filter{Sensor: in.Sensor, Label: in.Label}
	for _, name := range in.Types {
		t, ok := hwinfo.ParseReadingType(name)
		if !ok {
			return nil, readingsOutput{}, fmt.Errorf("unknown reading type %q; use any of: %s", name, strings.Join(hwinfo.TypeNames(), ", "))
		}
		filter.Types = append(filter.Types, t)
	}

	snap, err := h.load()
	if err != nil {
		return nil, readingsOutput{}, err
	}
	selected := snap.Select(filter)

	out := readingsOutput{
		Active:  snap.Active,
		Matched: len(selected),
		Sensors: bySensor(snap, selected, true, in.Stats),
	}
	out.LastUpdate, _, out.Warning = h.freshness(snap)
	return nil, out, nil
}

func (h *handlers) thermalOverview(_ context.Context, _ *mcp.CallToolRequest, _ noInput) (*mcp.CallToolResult, thermalOutput, error) {
	snap, err := h.load()
	if err != nil {
		return nil, thermalOutput{}, err
	}
	th := snap.Thermals()

	out := thermalOutput{
		Active: snap.Active,
		CPUs:   make([]cpuThermals, len(th.CPUs)),
		GPUs:   make([]gpuThermals, len(th.GPUs)),
		Other:  bySensor(snap, th.Other, false, true),
		Fans:   bySensor(snap, th.Fans, false, true),
	}
	out.LastUpdate, _, out.Warning = h.freshness(snap)

	for i, c := range th.CPUs {
		out.CPUs[i] = cpuThermals{
			Name:         c.Name,
			Temperature:  pick(c.Temperature),
			DieAverage:   pick(c.DieAverage),
			CCDs:         picks(c.CCDs),
			HottestCore:  pick(c.HottestCore),
			PackagePower: pick(c.PackagePower),
			Throttling:   throttled(c.Throttling),
		}
	}
	for i, g := range th.GPUs {
		out.GPUs[i] = gpuThermals{
			Name:           g.Name,
			Temperature:    pick(g.Temperature),
			HotSpot:        pick(g.HotSpot),
			MemoryJunction: pick(g.MemoryJunction),
			BoardPower:     pick(g.Power),
			Fans:           picks(g.Fans),
			Throttling:     throttled(g.Throttling),
		}
	}
	return nil, out, nil
}

// num prepares a value for JSON: rounded to the three decimals HWiNFO shows,
// and nil for NaN and the infinities, which JSON cannot express.
func num(v float64) *float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return nil
	}
	v = math.Round(v*1000) / 1000
	if v == 0 {
		v = 0 // turn a negative zero into a plain one
	}
	return &v
}

// displayName returns the name HWiNFO shows, or "" when it is the same as
// the original and repeating it would only add noise.
func displayName(shown, original string) string {
	if shown == original {
		return ""
	}
	return shown
}

func toReading(r hwinfo.Reading, withType, withStats bool) reading {
	out := reading{
		Label:   r.OriginalLabel,
		Display: displayName(r.Label, r.OriginalLabel),
		Value:   num(r.Value),
		Unit:    r.Unit,
	}
	if withType {
		out.Type = r.Type.String()
	}
	if withStats {
		out.Min, out.Max, out.Avg = num(r.Min), num(r.Max), num(r.Avg)
	}
	return out
}

// pick converts an optional overview reading, statistics included.
func pick(r *hwinfo.Reading) *reading {
	if r == nil {
		return nil
	}
	out := toReading(*r, false, true)
	return &out
}

func picks(rs []hwinfo.Reading) []reading {
	if len(rs) == 0 {
		return nil
	}
	out := make([]reading, len(rs))
	for i, r := range rs {
		out[i] = toReading(r, false, true)
	}
	return out
}

func throttled(t *hwinfo.Throttling) *throttling {
	if t == nil {
		return nil
	}
	return &throttling{Active: t.Active, Occurred: t.Occurred}
}

// bySensor groups readings under the sensor they belong to, keeping HWiNFO's
// order. The result is never nil, so it always encodes as a JSON array.
func bySensor(snap *hwinfo.Snapshot, rs []hwinfo.Reading, withType, withStats bool) []sensorReadings {
	groups := []sensorReadings{}
	position := map[int]int{} // sensor index -> position in groups
	for _, r := range rs {
		i, ok := position[r.SensorIndex]
		if !ok {
			i = len(groups)
			position[r.SensorIndex] = i
			g := sensorReadings{Sensor: "(no sensor)"}
			if s := snap.Sensor(r.SensorIndex); s != nil {
				g.Sensor, g.Display = s.OriginalName, displayName(s.Name, s.OriginalName)
			}
			groups = append(groups, g)
		}
		groups[i].Readings = append(groups[i].Readings, toReading(r, withType, withStats))
	}
	return groups
}
