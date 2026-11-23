package hwinfo

import (
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/templarfelix/hwinfo-mcp/internal/hwinfo/hwinfotest"
)

// The tests in this file run against the sensors of a real machine instead of
// labels somebody remembered. testdata/ryzen-9950x3d-rtx-5080.json is what
// get_readings returned on a Ryzen 9 9950X3D with a GeForce RTX 5080, under
// HWiNFO 8.60 running in Portuguese. Two things about that system broke
// assumptions that hold on an English HWiNFO 7:
//
//   - the displayed labels are translated ("GPU Temperature" is shown as
//     "Temperatura GPU"), so only the original names can be matched;
//   - the graphics card's sensor is called "dGPU [#0]: ...", not "GPU [#0]: ...".

type realReading struct {
	Label   string  `json:"label"`
	Display string  `json:"display"` // empty when HWiNFO shows the original label
	Type    string  `json:"type"`
	Unit    string  `json:"unit"`
	Value   float64 `json:"value"`
	Min     float64 `json:"min"`
	Max     float64 `json:"max"`
	Avg     float64 `json:"avg"`
}

type realSystem struct {
	Sensors []struct {
		Sensor   string        `json:"sensor"`
		Display  string        `json:"display"`
		Readings []realReading `json:"readings"`
	} `json:"sensors"`
}

func loadRealSystem(t *testing.T) realSystem {
	t.Helper()
	raw, err := os.ReadFile("testdata/ryzen-9950x3d-rtx-5080.json")
	if err != nil {
		t.Fatal(err)
	}
	var sys realSystem
	if err := json.Unmarshal(raw, &sys); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return sys
}

// image rebuilds the shared memory block the fixture was read from.
//
// With swapped set, every original name trades places with its displayed
// name. Nothing that depends on the displayed names may survive that.
func (sys realSystem) image(t *testing.T, layout int, swapped bool) hwinfotest.Image {
	t.Helper()
	names := func(original, displayed string) (string, string) {
		if displayed == "" {
			displayed = original
		}
		if swapped {
			return displayed, original
		}
		return original, displayed
	}

	im := hwinfotest.Image{Layout: layout, PollTime: 1795464598, PollingPeriod: 2000}
	for i, s := range sys.Sensors {
		name, user := names(s.Sensor, s.Display)
		im.Sensors = append(im.Sensors, hwinfotest.Sensor{Name: name, UserName: user})
		for _, r := range s.Readings {
			typ, ok := ParseReadingType(r.Type)
			if !ok {
				t.Fatalf("fixture: unknown reading type %q", r.Type)
			}
			label, userLabel := names(r.Label, r.Display)
			im.Readings = append(im.Readings, hwinfotest.Reading{
				Type: uint32(typ), Sensor: uint32(i),
				Label: label, UserLabel: userLabel, Unit: r.Unit,
				Value: r.Value, Min: r.Min, Max: r.Max, Avg: r.Avg,
			})
		}
	}
	return im
}

func (sys realSystem) snapshot(t *testing.T, layout int, swapped bool) *Snapshot {
	t.Helper()
	return parse(t, sys.image(t, layout, swapped))
}

// find returns the fixture's readings with the given original label and type.
func (sys realSystem) find(label, typ string) []realReading {
	var out []realReading
	for _, s := range sys.Sensors {
		for _, r := range s.Readings {
			if r.Label == label && r.Type == typ {
				out = append(out, r)
			}
		}
	}
	return out
}

// The fixture is only worth something while it still contains what makes the
// real system hard. This pins those properties, so that an edit to the file
// cannot quietly turn the tests below into easy ones.
func TestRealSystemFixture(t *testing.T) {
	sys := loadRealSystem(t)
	snap := sys.snapshot(t, 2, false)

	if len(snap.Sensors) != 19 || len(snap.Readings) != 484 {
		t.Fatalf("%d sensors and %d readings, want 19 and 484", len(snap.Sensors), len(snap.Readings))
	}
	byType := map[string]int{}
	translated := 0
	for _, r := range snap.Readings {
		byType[r.Type.String()]++
		if r.Label != r.OriginalLabel {
			translated++
		}
		if r.Type == TypeTemperature && r.Unit != degC {
			t.Errorf("%s: unit %q, want %q", r.OriginalLabel, r.Unit, degC)
		}
	}
	wantTypes := map[string]int{"temperature": 49, "voltage": 45, "fan": 5, "current": 5, "power": 36, "clock": 65, "usage": 137, "other": 142}
	if !reflect.DeepEqual(byType, wantTypes) {
		t.Errorf("readings per type = %v, want %v", byType, wantTypes)
	}
	if translated < 300 {
		t.Errorf("only %d labels differ from their original; the fixture must come from a translated HWiNFO", translated)
	}

	// Readings that look like the ones the overview wants, but are not.
	decoys := []struct{ label, typ, why string }{
		{"GPU Power Limit (rated)", "power", "a power reading that starts with \"GPU Power\""},
		{"GPU Power Limit (max)", "power", "a power reading that starts with \"GPU Power\""},
		{"GPU 12VHPWR Power", "power", "one rail's power, not the board's"},
		{"GPU Fan1", "other", "the fan's duty cycle in %, next to its speed in RPM"},
		{"L3 Cache (CCD1)", "temperature", "a per-CCD temperature that is not the CCD's"},
		{"L3 Cache (CCD1)", "clock", "the same label once more, as a clock"},
		{"Core0 (CCD1)", "temperature", "a core temperature that mentions a CCD"},
		{"GPU Memory A0 Temperature", "temperature", "one memory chip, not the junction maximum"},
		{"CPU", "temperature", "the mainboard's CPU socket sensor"},
		{"CPU PPT", "power", "the fallback for package power, which must lose to the real one"},
	}
	for _, d := range decoys {
		if len(sys.find(d.label, d.typ)) == 0 {
			t.Errorf("fixture lost the %s reading %q (%s)", d.typ, d.label, d.why)
		}
	}
	if n := len(sys.find("GPU Core (NVVDD) Output Power", "power")); n != 2 {
		t.Errorf("%d readings labelled GPU Core (NVVDD) Output Power, want the 2 identical labels HWiNFO reports", n)
	}

	cpuSensors := 0
	for _, s := range snap.Sensors {
		if strings.HasPrefix(s.OriginalName, "CPU [#0]: AMD Ryzen 9 9950X3D") {
			cpuSensors++
		}
	}
	if cpuSensors != 3 {
		t.Errorf("%d sensors belong to the CPU, want 3", cpuSensors)
	}
}

// The eight readings the thermal overview exists for, plus what surrounds
// them, picked out of the 484 the machine reports.
func TestThermalsOnRealSystem(t *testing.T) {
	sys := loadRealSystem(t)

	for _, layout := range []int{1, 2} {
		th := sys.snapshot(t, layout, false).Thermals()

		if len(th.CPUs) != 1 || len(th.GPUs) != 1 {
			t.Fatalf("layout %d: %d CPUs and %d GPUs, want 1 and 1", layout, len(th.CPUs), len(th.GPUs))
		}
		cpu, gpu := th.CPUs[0], th.GPUs[0]
		if cpu.Name != "AMD Ryzen 9 9950X3D" || gpu.Name != "NVIDIA GeForce RTX 5080" {
			t.Errorf("layout %d: devices %q and %q", layout, cpu.Name, gpu.Name)
		}

		picks := []struct {
			field     string
			got       *Reading
			original  string
			displayed string // what the Portuguese HWiNFO shows for it
			typ       ReadingType
			unit      string
			value     float64
		}{
			{"CPU temperature", cpu.Temperature, "CPU (Tctl/Tdie)", "CPU (Tctl/Tdie)", TypeTemperature, degC, 77.25},
			{"CPU die average", cpu.DieAverage, "CPU Die (average)", "Caso CPU (média)", TypeTemperature, degC, 74.469},
			{"CPU package power", cpu.PackagePower, "CPU Package Power", "Potência total da CPU", TypePower, "W", 155.223},
			{"GPU temperature", gpu.Temperature, "GPU Temperature", "Temperatura GPU", TypeTemperature, degC, 64.773},
			{"GPU hot spot", gpu.HotSpot, "GPU Hot Spot Temperature", "Temperatura de ponto quente da GPU", TypeTemperature, degC, 80.313},
			{"GPU memory junction", gpu.MemoryJunction, "GPU Memory Junction Temperature", "Temperatura de junção da memória GPU", TypeTemperature, degC, 68},
			{"GPU board power", gpu.Power, "GPU Power", "GPU Potência", TypePower, "W", 261.324},
		}
		for _, p := range picks {
			if p.got == nil {
				t.Errorf("layout %d: %s: nothing picked, want %q", layout, p.field, p.original)
				continue
			}
			if p.got.OriginalLabel != p.original || p.got.Label != p.displayed || p.got.Type != p.typ || p.got.Unit != p.unit || p.got.Value != p.value {
				t.Errorf("layout %d: %s: picked %q (%q) %s %v %s; want %q (%q) %s %v %s", layout, p.field,
					p.got.OriginalLabel, p.got.Label, p.got.Type, p.got.Value, p.got.Unit,
					p.original, p.displayed, p.typ, p.value, p.unit)
			}
		}

		if got, want := originals(cpu.CCDs), []string{"CPU CCD1 (Tdie)", "CPU CCD2 (Tdie)"}; !reflect.DeepEqual(got, want) {
			t.Errorf("layout %d: CCDs = %q, want %q", layout, got, want)
		} else if cpu.CCDs[0].Value != 67.5 || cpu.CCDs[1].Value != 72.375 {
			t.Errorf("layout %d: CCD temperatures = %v and %v", layout, cpu.CCDs[0].Value, cpu.CCDs[1].Value)
		}

		// The hottest core, worked out here independently of the patterns
		// under test: the sixteen "CoreN (CCDn)" temperatures of the fixture.
		coreLabel := regexp.MustCompile(`^Core\d+ \(CCD\d\)$`)
		var hottestCore realReading
		cores := 0
		for _, s := range sys.Sensors {
			for _, r := range s.Readings {
				if r.Type == "temperature" && coreLabel.MatchString(r.Label) {
					cores++
					if r.Value > hottestCore.Value {
						hottestCore = r
					}
				}
			}
		}
		if cores != 16 {
			t.Fatalf("fixture has %d core temperatures, want 16", cores)
		}
		if cpu.HottestCore == nil || cpu.HottestCore.OriginalLabel != hottestCore.Label || cpu.HottestCore.Value != hottestCore.Value {
			t.Errorf("layout %d: hottest core = %s, want %s at %v", layout, label(cpu.HottestCore), hottestCore.Label, hottestCore.Value)
		}

		// Speeds, not the duty cycles that carry the same labels.
		if got, want := originals(gpu.Fans), []string{"GPU Fan1", "GPU Fan2"}; !reflect.DeepEqual(got, want) {
			t.Errorf("layout %d: GPU fans = %q, want %q", layout, got, want)
		}
		for _, f := range gpu.Fans {
			if f.Type != TypeFan || f.Unit != "RPM" {
				t.Errorf("layout %d: GPU fan %q is a %s in %q, want a fan speed in RPM", layout, f.OriginalLabel, f.Type, f.Unit)
			}
		}

		// Nothing was throttling when the fixture was taken, but the flags
		// that say so were found on both devices.
		for device, got := range map[string]*Throttling{"CPU": cpu.Throttling, "GPU": gpu.Throttling} {
			if got == nil || got.Active || got.Occurred {
				t.Errorf("layout %d: %s throttling = %+v, want flags found and clear", layout, device, got)
			}
		}

		// Everything else that measures a temperature, including the
		// mainboard's own "CPU" sensor, which is not the processor's.
		wantOther := []string{
			"Chipset 1 (xHCI)", "Chipset 2 (xHCI)",
			"System1", "PCH", "CPU", "PCIEX16", "VRM MOS",
			"SPD Hub Temperature", "SPD Hub Temperature",
			"Drive Temperature", "Drive Temperature 3",
		}
		if got := originals(th.Other); !reflect.DeepEqual(got, wantOther) {
			t.Errorf("layout %d: other temperatures = %q, want %q", layout, got, wantOther)
		}
		if got, want := originals(th.Fans), []string{"System 2", "System 3", "System 4"}; !reflect.DeepEqual(got, want) {
			t.Errorf("layout %d: fans = %q, want %q", layout, got, want)
		}
	}
}

// The overview must read HWiNFO's original names and nothing else. Here the
// two name fields are swapped, so the original field holds the Portuguese
// text. Every reading HWiNFO translates must now go unrecognised; picking any
// of them would mean the displayed name was consulted, which is exactly what
// fails on a translated HWiNFO.
func TestThermalsIgnoreDisplayedNames(t *testing.T) {
	sys := loadRealSystem(t)
	th := sys.snapshot(t, 2, true).Thermals()

	// Sensor names of processors and graphics cards are not translated,
	// so the devices themselves are still found.
	if len(th.CPUs) != 1 || len(th.GPUs) != 1 {
		t.Fatalf("%d CPUs and %d GPUs, want 1 and 1", len(th.CPUs), len(th.GPUs))
	}
	cpu, gpu := th.CPUs[0], th.GPUs[0]

	translated := map[string]*Reading{
		"CPU die average":     cpu.DieAverage,
		"GPU temperature":     gpu.Temperature,
		"GPU hot spot":        gpu.HotSpot,
		"GPU memory junction": gpu.MemoryJunction,
		"GPU board power":     gpu.Power,
	}
	for field, got := range translated {
		if got != nil {
			t.Errorf("%s: picked %q by its displayed name", field, got.OriginalLabel)
		}
	}
	if cpu.Throttling != nil || gpu.Throttling != nil {
		t.Errorf("throttling flags were found by their displayed names: %+v, %+v", cpu.Throttling, gpu.Throttling)
	}

	// Labels HWiNFO leaves in English read the same in both fields and are
	// still found. Package power falls back to "CPU PPT" for the same
	// reason; it must not be the translated "CPU Package Power".
	if got := label(cpu.Temperature); got != "CPU (Tctl/Tdie)" {
		t.Errorf("CPU temperature = %q, want CPU (Tctl/Tdie)", got)
	}
	if got := label(cpu.PackagePower); got != "CPU PPT" {
		t.Errorf("CPU package power = %q, want the untranslated fallback CPU PPT", got)
	}
}

// Filters, unlike the overview, are typed by people and models who see either
// name, so they accept both.
func TestSelectOnRealSystem(t *testing.T) {
	snap := loadRealSystem(t).snapshot(t, 2, false)

	for _, q := range []string{"GPU Temperature", "temperatura gpu"} {
		got := snap.Select(Filter{Label: q})
		if len(got) != 1 || got[0].OriginalLabel != "GPU Temperature" || got[0].Label != "Temperatura GPU" {
			t.Errorf("label %q found %q", q, originals(got))
		}
	}

	gpuTemps := snap.Select(Filter{Sensor: "rtx 5080", Types: []ReadingType{TypeTemperature}})
	if len(gpuTemps) != 11 {
		t.Errorf("%d GPU temperatures, want 11: %q", len(gpuTemps), originals(gpuTemps))
	}

	// "CPU" as a sensor filter covers the processor's three sensors only;
	// the mainboard sensor that has a reading called "CPU" is not one.
	cpu := snap.Select(Filter{Sensor: "cpu"})
	sensors := map[string]bool{}
	for _, r := range cpu {
		sensors[snap.Sensors[r.SensorIndex].OriginalName] = true
	}
	if len(cpu) != 270 || len(sensors) != 3 {
		t.Errorf("sensor filter \"cpu\": %d readings from %d sensors, want 270 from 3", len(cpu), len(sensors))
	}

	// Renamed sensors are found by the name the user sees.
	if got := snap.Select(Filter{Sensor: "PresentMon [game.exe]"}); len(got) != 23 {
		t.Errorf("sensor filter by displayed name: %d readings, want PresentMon's 23", len(got))
	}
}
