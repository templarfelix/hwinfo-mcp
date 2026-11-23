package hwinfo

import (
	"reflect"
	"testing"

	"github.com/templarfelix/hwinfo-mcp/internal/hwinfo/hwinfotest"
)

// parse builds the block described by im and decodes it.
func parse(t *testing.T, im hwinfotest.Image) *Snapshot {
	t.Helper()
	snap, err := Parse(im.Bytes())
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return snap
}

// originals returns the original labels of rs, for compact comparisons.
func originals(rs []Reading) []string {
	out := []string{}
	for _, r := range rs {
		out = append(out, r.OriginalLabel)
	}
	return out
}

func label(r *Reading) string {
	if r == nil {
		return "<none>"
	}
	return r.OriginalLabel
}

func TestSelect(t *testing.T) {
	snap := parse(t, hwinfotest.Image{
		Sensors: []hwinfotest.Sensor{
			{Name: "CPU [#0]: AMD Ryzen 9 9950X3D: Enhanced"},
			{Name: "dGPU [#0]: NVIDIA GeForce RTX 5080"},
			{Name: "System: GIGABYTE X870E AORUS ELITE WIFI7", UserName: "Sistema: GIGABYTE X870E AORUS ELITE WIFI7"},
		},
		Readings: []hwinfotest.Reading{
			{Type: hwinfotest.Temperature, Sensor: 0, Label: "CPU (Tctl/Tdie)"},
			{Type: hwinfotest.Power, Sensor: 0, Label: "CPU Package Power", UserLabel: "Pot\u00eancia total da CPU"},
			{Type: hwinfotest.Temperature, Sensor: 1, Label: "GPU Temperature", UserLabel: "Temperatura GPU"},
			{Type: hwinfotest.Fan, Sensor: 1, Label: "GPU Fan1", UserLabel: "GPU Ventilador1"},
			{Type: hwinfotest.Other, Sensor: 1, Label: "GPU Fan1", UserLabel: "GPU Ventilador1"},
			{Type: hwinfotest.Other, Sensor: 2, Label: "Physical Memory Load", UserLabel: "Carga da mem\u00f3ria f\u00edsica"},
			{Type: hwinfotest.Temperature, Sensor: 99, Label: "Orphan"},
		},
	})

	tests := []struct {
		name   string
		filter Filter
		want   []string
	}{
		{"no filter", Filter{}, []string{"CPU (Tctl/Tdie)", "CPU Package Power", "GPU Temperature", "GPU Fan1", "GPU Fan1", "Physical Memory Load", "Orphan"}},
		{"sensor, any case", Filter{Sensor: "rtx 5080"}, []string{"GPU Temperature", "GPU Fan1", "GPU Fan1"}},
		{"sensor by original name", Filter{Sensor: "System:"}, []string{"Physical Memory Load"}},
		{"sensor by displayed name", Filter{Sensor: "sistema"}, []string{"Physical Memory Load"}},
		{"sensor filter drops readings without a sensor", Filter{Sensor: "e"}, []string{"CPU (Tctl/Tdie)", "CPU Package Power", "GPU Temperature", "GPU Fan1", "GPU Fan1", "Physical Memory Load"}},
		{"label by original name", Filter{Label: "package power"}, []string{"CPU Package Power"}},
		{"label by displayed name", Filter{Label: "POT\u00caNCIA"}, []string{"CPU Package Power"}},
		{"label with surrounding space", Filter{Label: "  tctl "}, []string{"CPU (Tctl/Tdie)"}},
		{"one type", Filter{Types: []ReadingType{TypeFan}}, []string{"GPU Fan1"}},
		{"several types", Filter{Types: []ReadingType{TypePower, TypeFan}}, []string{"CPU Package Power", "GPU Fan1"}},
		{"everything combined", Filter{Sensor: "gpu", Label: "fan", Types: []ReadingType{TypeOther}}, []string{"GPU Fan1"}},
		{"no match", Filter{Label: "liquid"}, []string{}},
		{"sensor and label that never meet", Filter{Sensor: "cpu", Label: "GPU Temperature"}, []string{}},
	}
	for _, tt := range tests {
		if got := originals(snap.Select(tt.filter)); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s:\n got %q\nwant %q", tt.name, got, tt.want)
		}
	}
}

// An Intel system with an integrated and a discrete adapter, named the way
// HWiNFO 7 did (plain "GPU"). None of the first-choice AMD labels exist, so
// every pick has to come from a fallback pattern.
func TestThermalsFallbacks(t *testing.T) {
	const degC = "\u00b0C"
	snap := parse(t, hwinfotest.Image{
		Sensors: []hwinfotest.Sensor{
			{Name: "CPU [#0]: Intel Core i9-13900K"},
			{Name: "CPU [#0]: Intel Core i9-13900K: DTS"},
			{Name: "CPU [#0]: Intel Core i9-13900K: Enhanced"},
			{Name: "GPU [#0]: Intel UHD Graphics 770"},
			{Name: "GPU [#1]: AMD Radeon RX 7900 XTX"},
			{Name: "ASUS ROG STRIX Z790-E (Nuvoton NCT6798D)"},
		},
		Readings: []hwinfotest.Reading{
			{Type: hwinfotest.Clock, Sensor: 0, Label: "Core 0 Clock", Unit: "MHz", Value: 5500},
			{Type: hwinfotest.Temperature, Sensor: 1, Label: "P-core 0", Unit: degC, Value: 61},
			{Type: hwinfotest.Temperature, Sensor: 1, Label: "P-core 1", Unit: degC, Value: 78},
			{Type: hwinfotest.Temperature, Sensor: 1, Label: "E-core 8", Unit: degC, Value: 55},
			{Type: hwinfotest.Temperature, Sensor: 1, Label: "Core 0 Distance to TjMAX", Unit: degC, Value: 39},
			{Type: hwinfotest.Temperature, Sensor: 1, Label: "Core Max", Unit: degC, Value: 78},
			{Type: hwinfotest.Temperature, Sensor: 1, Label: "CPU Package", Unit: degC, Value: 79},
			{Type: hwinfotest.Other, Sensor: 1, Label: "Core Thermal Throttling", Unit: "Yes/No", Value: 0, Max: 1},
			{Type: hwinfotest.Power, Sensor: 2, Label: "IA Cores Power", Unit: "W", Value: 180},
			{Type: hwinfotest.Power, Sensor: 2, Label: "CPU Package Power", Unit: "W", Value: 201},
			{Type: hwinfotest.Temperature, Sensor: 3, Label: "GPU Temperature", Unit: degC, Value: 48},
			{Type: hwinfotest.Power, Sensor: 3, Label: "GPU Power", Unit: "W", Value: 3},
			{Type: hwinfotest.Temperature, Sensor: 4, Label: "GPU Temperature", Unit: degC, Value: 66},
			{Type: hwinfotest.Temperature, Sensor: 4, Label: "GPU Hot Spot Temperature", Unit: degC, Value: 84},
			{Type: hwinfotest.Temperature, Sensor: 4, Label: "GPU Memory Temperature", Unit: degC, Value: 72},
			{Type: hwinfotest.Power, Sensor: 4, Label: "GPU Core Power (VDDCR_GFX)", Unit: "W", Value: 190},
			{Type: hwinfotest.Power, Sensor: 4, Label: "GPU PPT", Unit: "W", Value: 290},
			{Type: hwinfotest.Power, Sensor: 4, Label: "Total Board Power (TBP)", Unit: "W", Value: 331},
			{Type: hwinfotest.Fan, Sensor: 4, Label: "GPU Fan", Unit: "RPM", Value: 1400},
			{Type: hwinfotest.Temperature, Sensor: 5, Label: "Motherboard", Unit: degC, Value: 34},
			{Type: hwinfotest.Temperature, Sensor: 5, Label: "CPU", Unit: degC, Value: 77},
			{Type: hwinfotest.Fan, Sensor: 5, Label: "CPU", Unit: "RPM", Value: 900},
		},
	})
	th := snap.Thermals()

	if len(th.CPUs) != 1 {
		t.Fatalf("%d CPUs, want the three CPU sensors folded into 1", len(th.CPUs))
	}
	cpu := th.CPUs[0]
	if cpu.Name != "Intel Core i9-13900K" {
		t.Errorf("CPU name = %q", cpu.Name)
	}
	// "CPU Package" is listed before "Core Max", so it wins although HWiNFO
	// lists "Core Max" first.
	if got := label(cpu.Temperature); got != "CPU Package" {
		t.Errorf("CPU temperature = %q, want CPU Package", got)
	}
	if got := label(cpu.HottestCore); got != "P-core 1" {
		t.Errorf("hottest core = %q, want P-core 1", got)
	}
	if got := label(cpu.PackagePower); got != "CPU Package Power" {
		t.Errorf("package power = %q", got)
	}
	if cpu.DieAverage != nil || len(cpu.CCDs) != 0 {
		t.Errorf("an Intel CPU got AMD-only fields: %v, %v", label(cpu.DieAverage), originals(cpu.CCDs))
	}
	if cpu.Throttling == nil || cpu.Throttling.Active || !cpu.Throttling.Occurred {
		t.Errorf("throttling = %+v, want not active but occurred", cpu.Throttling)
	}

	if len(th.GPUs) != 2 {
		t.Fatalf("%d GPUs, want 2", len(th.GPUs))
	}
	igpu, dgpu := th.GPUs[0], th.GPUs[1]
	if igpu.Name != "Intel UHD Graphics 770" || dgpu.Name != "AMD Radeon RX 7900 XTX" {
		t.Errorf("GPU names = %q, %q", igpu.Name, dgpu.Name)
	}
	if igpu.Temperature == nil || igpu.Temperature.Value != 48 || dgpu.Temperature == nil || dgpu.Temperature.Value != 66 {
		t.Error("the two GPUs' temperatures got mixed up")
	}
	if igpu.HotSpot != nil || igpu.MemoryJunction != nil || len(igpu.Fans) != 0 || igpu.Throttling != nil {
		t.Error("the integrated GPU got readings it does not have")
	}
	if got := label(dgpu.HotSpot); got != "GPU Hot Spot Temperature" {
		t.Errorf("hot spot = %q", got)
	}
	if got := label(dgpu.MemoryJunction); got != "GPU Memory Temperature" {
		t.Errorf("memory temperature = %q, want the fallback GPU Memory Temperature", got)
	}
	// Board power is preferred over PPT even though PPT comes first.
	if got := label(dgpu.Power); got != "Total Board Power (TBP)" {
		t.Errorf("board power = %q, want Total Board Power (TBP)", got)
	}
	if got := originals(dgpu.Fans); !reflect.DeepEqual(got, []string{"GPU Fan"}) {
		t.Errorf("GPU fans = %q", got)
	}

	if got := originals(th.Other); !reflect.DeepEqual(got, []string{"Motherboard", "CPU"}) {
		t.Errorf("other temperatures = %q, want only the mainboard's", got)
	}
	if got := originals(th.Fans); !reflect.DeepEqual(got, []string{"CPU"}) {
		t.Errorf("fans = %q, want only the mainboard's", got)
	}
}

func TestThermalsWithoutCPUOrGPUSensors(t *testing.T) {
	snap := parse(t, hwinfotest.Image{
		Sensors: []hwinfotest.Sensor{{Name: "S.M.A.R.T.: Some Drive"}},
		Readings: []hwinfotest.Reading{
			{Type: hwinfotest.Temperature, Sensor: 0, Label: "Drive Temperature", Value: 40},
			{Type: hwinfotest.Usage, Sensor: 0, Label: "Drive Remaining Life", Value: 97},
		},
	})
	th := snap.Thermals()
	if len(th.CPUs) != 0 || len(th.GPUs) != 0 || len(th.Fans) != 0 {
		t.Errorf("thermals = %+v, want no CPUs, GPUs or fans", th)
	}
	if got := originals(th.Other); !reflect.DeepEqual(got, []string{"Drive Temperature"}) {
		t.Errorf("other temperatures = %q", got)
	}

	empty := (&Snapshot{}).Thermals()
	if len(empty.CPUs)+len(empty.GPUs)+len(empty.Other)+len(empty.Fans) != 0 {
		t.Errorf("thermals of an empty snapshot = %+v", empty)
	}
}

// A sensor merely mentioning a CPU or GPU is not one.
func TestDeviceSensorPatterns(t *testing.T) {
	cpus := map[string]string{
		"CPU [#0]: AMD Ryzen 9 9950X3D":                    "AMD Ryzen 9 9950X3D",
		"CPU [#0]: AMD Ryzen 9 9950X3D: Enhanced":          "AMD Ryzen 9 9950X3D",
		"CPU [#0]: AMD Ryzen 9 9950X3D: C-State Residency": "AMD Ryzen 9 9950X3D",
		"CPU [#1]: Intel Xeon Gold 6248R: DTS":             "Intel Xeon Gold 6248R",
		"GIGABYTE X870E AORUS ELITE WIFI7 (ITE IT8696E)":   "",
		"dGPU [#0]: NVIDIA GeForce RTX 5080":               "",
		"System: CPU [#0]: fake":                           "",
	}
	for name, want := range cpus {
		got := ""
		if m := cpuSensor.FindStringSubmatch(name); m != nil {
			got = m[1]
		}
		if got != want {
			t.Errorf("cpuSensor on %q = %q, want %q", name, got, want)
		}
	}

	gpus := map[string]string{
		"dGPU [#0]: NVIDIA GeForce RTX 5080":        "NVIDIA GeForce RTX 5080",
		"iGPU [#0]: AMD Radeon Graphics":            "AMD Radeon Graphics",
		"eGPU [#1]: NVIDIA GeForce RTX 4070":        "NVIDIA GeForce RTX 4070",
		"GPU [#0]: NVIDIA GeForce RTX 3080":         "NVIDIA GeForce RTX 3080",
		"GPU [#0]: AMD Radeon RX 6800 XT: CHiL/IR":  "AMD Radeon RX 6800 XT",
		"CPU [#0]: AMD Ryzen 9 9950X3D":             "",
		"PresentMon":                                "",
		"xGPU [#0]: nothing HWiNFO would ever name": "",
	}
	for name, want := range gpus {
		got := ""
		if m := gpuSensor.FindStringSubmatch(name); m != nil {
			got = m[1]
		}
		if got != want {
			t.Errorf("gpuSensor on %q = %q, want %q", name, got, want)
		}
	}
}
