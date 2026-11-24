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

Start with thermal_overview for the key CPU and GPU temperatures. For anything else, call list_sensors to see what exists, then get_readings with a sensor, label or types filter: there are hundreds of readings and one call returns at most 300; next_offset says where to continue.

Whether a reading ever went too high or too low is answered by its minimum and maximum (get_readings with stats), which cover every poll since HWiNFO started. sample_readings watches readings for a few seconds instead and tells when their extremes happened, so that they can be set against what other readings did at that moment.

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
	src   source
	now   func() time.Time
	sleep func(context.Context, time.Duration) error
}

// newServer builds the MCP server around src.
func newServer(src source) *mcp.Server {
	return newServerAt(src, time.Now, sleepFor)
}

// sleepFor waits for d, or returns early with the context's error.
func sleepFor(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// newServerAt is newServer with a replaceable clock, which decides whether a
// snapshot counts as fresh, and a replaceable wait, which paces sampling.
func newServerAt(src source, now func() time.Time, sleep func(context.Context, time.Duration) error) *mcp.Server {
	h := &handlers{src: src, now: now, sleep: sleep}

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
		Description: "Return current sensor readings from HWiNFO, grouped by sensor. Narrow the result with the sensor, label and types filters: a PC has several hundred readings and one call returns at most 300 of them. When more matched, truncated is set and next_offset is the offset to pass for the following ones. Set stats to add the minimum, maximum and average HWiNFO has recorded since it started.",
		Annotations: readOnly("Get HWiNFO readings"),
	}, h.getReadings)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "sample_readings",
		Description: "Watch readings for a few seconds and summarise what they did: first and last value, lowest, highest and mean, the largest change between two samples, and when each of those happened. It takes one sample per HWiNFO poll (every 2 seconds unless the user changed it), so anything shorter than a poll is invisible. It does not find extremes that get_readings with stats would miss: HWiNFO's own minimum and maximum already cover every poll since it started. Use it to learn when something happened and to set readings against each other at the same moment, for example a voltage against the power drawn while a load starts. Takes the sensor, label and types filters of get_readings, watches at most 150 readings for at most 30 seconds, and blocks for that long.",
		Annotations: readOnly("Sample HWiNFO readings over time"),
	}, h.sampleReadings)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "thermal_overview",
		Description: "Summarise how hot the PC runs: for each CPU the control temperature (Tctl/Tdie or package), die average, per-CCD temperatures, hottest core, package power and how close it runs to its temperature limit; for each GPU the core, hot spot and memory junction temperatures, power draw and fans; plus the remaining temperatures (mainboard, memory, drives) and fan speeds. Every reading includes its minimum, maximum and average since HWiNFO started.",
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
	Offset int      `json:"offset,omitempty" jsonschema:"number of matching readings to skip; pass the next_offset of the previous call to read on"`
}

type sampleInput struct {
	Sensor  string   `json:"sensor,omitempty" jsonschema:"case-insensitive part of a sensor name, such as CPU or RTX 5080; matches the original and the displayed name"`
	Label   string   `json:"label,omitempty" jsonschema:"case-insensitive part of a reading label, such as +12V or Tctl; matches the original and the displayed label"`
	Types   []string `json:"types,omitempty" jsonschema:"reading types to include, any of: temperature, voltage, fan, current, power, clock, usage, other"`
	Seconds int      `json:"seconds,omitempty" jsonschema:"how long to watch, from 1 to 30 seconds; 10 if omitted"`
	Values  bool     `json:"values,omitempty" jsonschema:"also return every sampled value with the time of each sample; only done when at most 16 readings match"`
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
	Matched    int              `json:"matched" jsonschema:"number of readings that match the filters"`
	Truncated  bool             `json:"truncated,omitempty" jsonschema:"true when readings remain after the ones returned; narrow the filters or continue at next_offset"`
	NextOffset int              `json:"next_offset,omitempty" jsonschema:"offset to pass to get the readings that follow; given when truncated"`
	Sensors    []sensorReadings `json:"sensors"`
}

// maxReadings caps what one get_readings call returns. A PC easily has 500
// readings, and with statistics that is more JSON than MCP clients accept in
// a single tool result; the cap keeps the largest answer around 40 KB.
const maxReadings = 300

// Limits of sample_readings. A call blocks for the whole window, and clients
// give up on a tool call after a time this server cannot know, so the window
// stays well under a minute. The other limits bound the size of the answer.
const (
	defaultSampleSeconds = 10
	maxSampleSeconds     = 30
	maxSampled           = 150 // readings one call watches
	maxSeries            = 16  // readings whose every sampled value is returned
	maxSamples           = 120 // samples one call takes
)

type sampledReading struct {
	Label   string     `json:"label" jsonschema:"HWiNFO's original English label"`
	Display string     `json:"display,omitempty" jsonschema:"label shown in HWiNFO when it was translated or renamed"`
	Type    string     `json:"type,omitempty" jsonschema:"temperature, voltage, fan, current, power, clock, usage or other"`
	Unit    string     `json:"unit,omitempty"`
	First   *float64   `json:"first" jsonschema:"value at the first sample; null, like the other values, if HWiNFO held no finite number during the window"`
	Last    *float64   `json:"last" jsonschema:"value at the last sample"`
	Min     *float64   `json:"min" jsonschema:"lowest sampled value"`
	Max     *float64   `json:"max" jsonschema:"highest sampled value"`
	Mean    *float64   `json:"mean" jsonschema:"mean of the sampled values"`
	MinAt   *float64   `json:"min_at,omitempty" jsonschema:"seconds into the window at which the lowest value was first sampled"`
	MaxAt   *float64   `json:"max_at,omitempty" jsonschema:"seconds into the window at which the highest value was first sampled"`
	Step    *float64   `json:"largest_step,omitempty" jsonschema:"largest change between two consecutive samples, as an absolute value"`
	StepAt  *float64   `json:"largest_step_at,omitempty" jsonschema:"seconds into the window of the sample that ended that change"`
	Values  []*float64 `json:"values,omitempty" jsonschema:"every sampled value, in the order of sample_times"`
}

type sampledSensor struct {
	Sensor   string           `json:"sensor" jsonschema:"HWiNFO's original English sensor name"`
	Display  string           `json:"display,omitempty" jsonschema:"name shown in HWiNFO when it was translated or renamed"`
	Readings []sampledReading `json:"readings"`
}

type sampleOutput struct {
	Started         string          `json:"started" jsonschema:"when the window began, RFC 3339"`
	Seconds         *float64        `json:"seconds" jsonschema:"how long the readings were watched"`
	Samples         int             `json:"samples" jsonschema:"number of samples taken, one per HWiNFO poll"`
	PollingPeriodMS int64           `json:"polling_period_ms,omitempty" jsonschema:"HWiNFO's sensor polling interval, which is the finest detail a sample can show"`
	SampleTimes     []*float64      `json:"sample_times,omitempty" jsonschema:"seconds into the window of each sample; given together with values"`
	Matched         int             `json:"matched" jsonschema:"number of readings that match the filters"`
	Truncated       bool            `json:"truncated,omitempty" jsonschema:"true when more readings matched than one call watches; narrow the filters"`
	Warning         string          `json:"warning,omitempty" jsonschema:"set when the window ended early or the values asked for were left out"`
	Sensors         []sampledSensor `json:"sensors"`
}

// throttling carries HWiNFO's Yes/No thermal flags and nothing more. A CPU
// can be held at its temperature limit with every flag clear, which is what
// cpuThermals.ThermalLimit is there to show.
type throttling struct {
	Active   bool `json:"active" jsonschema:"one of HWiNFO's thermal throttling flags is set right now"`
	Occurred bool `json:"occurred" jsonschema:"one of those flags was set at some point since HWiNFO started"`
}

type cpuThermals struct {
	Name         string      `json:"name"`
	Temperature  *reading    `json:"temperature,omitempty" jsonschema:"the temperature the CPU regulates itself by: Tctl/Tdie on AMD, package on Intel"`
	DieAverage   *reading    `json:"die_average,omitempty"`
	CCDs         []reading   `json:"ccds,omitempty" jsonschema:"temperature of each core complex die (AMD)"`
	HottestCore  *reading    `json:"hottest_core,omitempty" jsonschema:"the core that is hottest right now"`
	PackagePower *reading    `json:"package_power,omitempty"`
	ThermalLimit *reading    `json:"thermal_limit,omitempty" jsonschema:"how much of its temperature limit the CPU uses, in percent (AMD); at 100 it is being held at the limit, which the throttling flags do not show"`
	Throttling   *throttling `json:"thermal_throttling,omitempty" jsonschema:"HWiNFO's thermal throttling flags; clear flags do not rule out a CPU held at its temperature limit, see thermal_limit"`
}

type gpuThermals struct {
	Name           string      `json:"name"`
	Temperature    *reading    `json:"temperature,omitempty" jsonschema:"GPU core temperature"`
	HotSpot        *reading    `json:"hot_spot,omitempty" jsonschema:"hottest point of the GPU die"`
	MemoryJunction *reading    `json:"memory_junction,omitempty" jsonschema:"hottest video memory chip"`
	Power          *reading    `json:"power,omitempty" jsonschema:"power the graphics card draws: the whole board where HWiNFO reports it, otherwise the GPU chip alone; the label says which"`
	Fans           []reading   `json:"fans,omitempty"`
	Throttling     *throttling `json:"thermal_throttling,omitempty" jsonschema:"HWiNFO's flag for a thermal performance limit on the card"`
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

// manualOnly ends the hints whose fix lies in HWiNFO's own settings. HWiNFO's
// licence prohibits automating actions that get around the limits of its free
// edition, and an assistant that can operate the desktop could otherwise take
// "re-enable the option" as something to do itself.
const manualOnly = ` Leave these steps to the user: HWiNFO's licence prohibits automating actions that get around its 12-hour limit.`

// hint says how to fix the condition err describes, or "" if there is no
// specific advice.
func hint(err error) string {
	switch {
	case errors.Is(err, hwinfo.ErrNotFound):
		return `HWiNFO is not publishing sensor data. The user has to make sure HWiNFO is running with the Sensors window open and "Shared Memory Support" enabled in its settings, and to restart HWiNFO if the option was switched on while it was already running. The free edition switches the option off again after 12 hours.` + manualOnly
	case errors.Is(err, hwinfo.ErrInactive):
		return `HWiNFO stopped updating its shared memory: the Sensors window was closed or the free edition's 12-hour limit was reached. The user has to reopen the Sensors window, re-enable "Shared Memory Support" or restart HWiNFO.` + manualOnly
	case errors.Is(err, hwinfo.ErrAccessDenied):
		return "Windows refused this server access to HWiNFO's shared memory, which points to HWiNFO and the MCP client running at different integrity levels. Leave this to the user: do not start the client or HWiNFO with other privileges yourself."
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
	if snap != nil && (err == nil || errors.Is(err, hwinfo.ErrInactive)) {
		return snap, nil
	}
	if err == nil {
		err = errors.New("no sensor data")
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

// parseFilter turns the filter arguments shared by get_readings and
// sample_readings into a filter, rejecting reading types that do not exist.
func parseFilter(sensor, label string, types []string) (hwinfo.Filter, error) {
	filter := hwinfo.Filter{Sensor: sensor, Label: label}
	for _, name := range types {
		t, ok := hwinfo.ParseReadingType(name)
		if !ok {
			return filter, fmt.Errorf("unknown reading type %q; use any of: %s", name, strings.Join(hwinfo.TypeNames(), ", "))
		}
		filter.Types = append(filter.Types, t)
	}
	return filter, nil
}

func (h *handlers) getReadings(_ context.Context, _ *mcp.CallToolRequest, in readingsInput) (*mcp.CallToolResult, readingsOutput, error) {
	filter, err := parseFilter(in.Sensor, in.Label, in.Types)
	if err != nil {
		return nil, readingsOutput{}, err
	}
	if in.Offset < 0 {
		return nil, readingsOutput{}, fmt.Errorf("offset %d is negative; pass 0 or the next_offset of a previous call", in.Offset)
	}

	snap, err := h.load()
	if err != nil {
		return nil, readingsOutput{}, err
	}
	selected := snap.Select(filter)

	out := readingsOutput{Active: snap.Active, Matched: len(selected)}
	selected = selected[min(in.Offset, len(selected)):]
	if len(selected) > maxReadings {
		selected = selected[:maxReadings]
		out.Truncated = true
		out.NextOffset = in.Offset + maxReadings
	}
	out.Sensors = bySensor(snap, selected, true, in.Stats)
	out.LastUpdate, _, out.Warning = h.freshness(snap)
	return nil, out, nil
}

// A track follows one reading through a sampling window. The reading is
// identified by where it sits and by what it is called: HWiNFO adds and
// removes sensors while it runs (the one for the application PresentMon is
// watching, for instance), which moves every reading after them.
type track struct {
	index  int // position in Snapshot.Readings
	sensor string
	label  string
	typ    hwinfo.ReadingType

	n           int
	first, last float64
	min, max    float64
	sum         float64
	minAt       float64
	maxAt       float64
	step        float64
	stepAt      float64
	values      []*float64 // only kept when every value was asked for
}

func (t *track) add(v, at float64, keep bool) {
	if keep {
		t.values = append(t.values, num(v))
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return
	}
	if t.n == 0 {
		t.first, t.min, t.max, t.minAt, t.maxAt = v, v, v, at, at
	} else {
		if v < t.min {
			t.min, t.minAt = v, at
		}
		if v > t.max {
			t.max, t.maxAt = v, at
		}
		if d := math.Abs(v - t.last); d > t.step {
			t.step, t.stepAt = d, at
		}
	}
	t.last = v
	t.sum += v
	t.n++
}

// follows reports whether snap still has every tracked reading where it was.
func follows(snap *hwinfo.Snapshot, tracks []*track) bool {
	for _, t := range tracks {
		if t.index >= len(snap.Readings) {
			return false
		}
		r := snap.Readings[t.index]
		name := ""
		if s := snap.Sensor(r.SensorIndex); s != nil {
			name = s.OriginalName
		}
		if r.OriginalLabel != t.label || r.Type != t.typ || name != t.sensor {
			return false
		}
	}
	return true
}

func (h *handlers) sampleReadings(ctx context.Context, _ *mcp.CallToolRequest, in sampleInput) (*mcp.CallToolResult, sampleOutput, error) {
	filter, err := parseFilter(in.Sensor, in.Label, in.Types)
	if err != nil {
		return nil, sampleOutput{}, err
	}
	seconds := in.Seconds
	if seconds == 0 {
		seconds = defaultSampleSeconds
	}
	if seconds < 1 || seconds > maxSampleSeconds {
		return nil, sampleOutput{}, fmt.Errorf("seconds is %d; a window lasts from 1 to %d seconds", in.Seconds, maxSampleSeconds)
	}
	window := time.Duration(seconds) * time.Second

	snap, err := h.load()
	if err != nil {
		return nil, sampleOutput{}, err
	}
	if !snap.Active {
		return nil, sampleOutput{}, fmt.Errorf("HWiNFO is not updating its sensors, so there is nothing to sample. %s", hint(hwinfo.ErrInactive))
	}

	selected := snap.Select(filter)
	out := sampleOutput{Matched: len(selected), PollingPeriodMS: snap.PollingPeriod.Milliseconds()}
	if len(selected) > maxSampled {
		selected = selected[:maxSampled]
		out.Truncated = true
	}
	keep := in.Values && len(selected) <= maxSeries
	if in.Values && !keep {
		out.Warning = fmt.Sprintf("Every sampled value is only returned for up to %d readings, and %d are being watched; narrow the filters to get them.", maxSeries, len(selected))
	}

	tracks := make([]*track, len(selected))
	for i, r := range selected {
		tracks[i] = &track{index: r.Index, label: r.OriginalLabel, typ: r.Type}
		if s := snap.Sensor(r.SensorIndex); s != nil {
			tracks[i].sensor = s.OriginalName
		}
	}

	start := h.now()
	out.Started = start.Local().Format(time.RFC3339)
	record := func(s *hwinfo.Snapshot, at float64) {
		for _, t := range tracks {
			t.add(s.Readings[t.index].Value, at, keep)
		}
		if keep {
			out.SampleTimes = append(out.SampleTimes, num(at))
		}
		out.Samples++
	}
	record(snap, 0)

	// One sample per poll. The poll time has one-second resolution, so with a
	// faster polling period it cannot tell two polls apart, and the period
	// itself paces the samples.
	period := snap.PollingPeriod
	if period <= 0 {
		period = time.Second
	}
	tick := min(max(period/4, 100*time.Millisecond), 500*time.Millisecond)
	lastPoll, lastAt := snap.PollTime, start
	elapsed := time.Duration(0)
	note := func(format string, args ...any) {
		text := fmt.Sprintf(format, args...)
		if out.Warning != "" {
			text = out.Warning + " " + text
		}
		out.Warning = text
	}
	for elapsed < window {
		if err := h.sleep(ctx, tick); err != nil {
			return nil, sampleOutput{}, err
		}
		now := h.now()
		if elapsed = now.Sub(start); elapsed >= window {
			break
		}
		cur, err := h.src()
		if cur == nil || err != nil {
			if err == nil {
				err = errors.New("no sensor data")
			}
			note("Sampling stopped after %s: %v. %s", elapsed.Round(time.Second), err, hint(err))
			break
		}
		if cur.PollTime.Equal(lastPoll) && (period >= time.Second || now.Sub(lastAt) < period) {
			continue // still the poll already sampled
		}
		if !follows(cur, tracks) {
			note("Sampling stopped after %s because HWiNFO's list of sensors changed, which happens when a sensor appears or goes away; call again to watch the new list.", elapsed.Round(time.Second))
			break
		}
		record(cur, elapsed.Seconds())
		lastPoll, lastAt = cur.PollTime, now
		if out.Samples >= maxSamples {
			note("Sampling stopped after %s, at the limit of %d samples.", elapsed.Round(time.Second), maxSamples)
			break
		}
	}
	out.Seconds = num(min(elapsed, window).Seconds())

	out.Sensors = []sampledSensor{}
	position := map[int]int{} // sensor index -> position in out.Sensors
	for i, t := range tracks {
		r := selected[i]
		at, ok := position[r.SensorIndex]
		if !ok {
			at = len(out.Sensors)
			position[r.SensorIndex] = at
			g := sampledSensor{Sensor: "(no sensor)"}
			if s := snap.Sensor(r.SensorIndex); s != nil {
				g.Sensor, g.Display = s.OriginalName, displayName(s.Name, s.OriginalName)
			}
			out.Sensors = append(out.Sensors, g)
		}
		sr := sampledReading{
			Label:   r.OriginalLabel,
			Display: displayName(r.Label, r.OriginalLabel),
			Type:    r.Type.String(),
			Unit:    r.Unit,
			Values:  t.values,
		}
		if t.n > 0 {
			sr.First, sr.Last = num(t.first), num(t.last)
			sr.Min, sr.Max = num(t.min), num(t.max)
			sr.Mean = num(t.sum / float64(t.n))
			sr.MinAt, sr.MaxAt = num(t.minAt), num(t.maxAt)
			if t.n > 1 {
				sr.Step = num(t.step)
				if t.step > 0 {
					sr.StepAt = num(t.stepAt)
				}
			}
		}
		out.Sensors[at].Readings = append(out.Sensors[at].Readings, sr)
	}
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
			ThermalLimit: pick(c.ThermalLimit),
			Throttling:   throttled(c.Throttling),
		}
	}
	for i, g := range th.GPUs {
		out.GPUs[i] = gpuThermals{
			Name:           g.Name,
			Temperature:    pick(g.Temperature),
			HotSpot:        pick(g.HotSpot),
			MemoryJunction: pick(g.MemoryJunction),
			Power:          pick(g.Power),
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
