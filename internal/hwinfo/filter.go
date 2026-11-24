package hwinfo

import (
	"math"
	"regexp"
	"slices"
	"strings"
)

// fold normalises a string for case-insensitive comparison.
func fold(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

// A Filter selects readings. A zero field matches everything.
type Filter struct {
	// Sensor and Label are case-insensitive substrings. Each is compared
	// with both the original English name and the name HWiNFO displays, so
	// "GPU Temperature" and "Temperatura GPU" find the same reading on a
	// Portuguese HWiNFO.
	Sensor string
	Label  string

	// Types lists the reading types to keep. Empty keeps all of them.
	Types []ReadingType
}

func containsFold(q string, names ...string) bool {
	if q == "" {
		return true
	}
	for _, n := range names {
		if strings.Contains(fold(n), q) {
			return true
		}
	}
	return false
}

// Select returns the readings that match f, in HWiNFO's order.
func (s *Snapshot) Select(f Filter) []Reading {
	sensorQ, labelQ := fold(f.Sensor), fold(f.Label)

	// Decide once per sensor rather than once per reading.
	sensorOK := make([]bool, len(s.Sensors))
	for i, sn := range s.Sensors {
		sensorOK[i] = containsFold(sensorQ, sn.OriginalName, sn.Name)
	}

	var out []Reading
	for _, r := range s.Readings {
		if len(f.Types) > 0 && !slices.Contains(f.Types, r.Type) {
			continue
		}
		if r.SensorIndex >= 0 {
			if !sensorOK[r.SensorIndex] {
				continue
			}
		} else if sensorQ != "" {
			continue // a reading without a sensor cannot match a sensor filter
		}
		if !containsFold(labelQ, r.OriginalLabel, r.Label) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// Thermal overview
//
// The heuristics below pick, out of hundreds of readings, the handful that
// say how hot the processor and the graphics card run. They match HWiNFO's
// original names only. Those are always English, whereas the displayed names
// follow HWiNFO's language: a Portuguese HWiNFO shows "GPU Temperature" as
// "Temperatura GPU" and "CPU Package Power" as "Potência total da CPU".

// A labelSet is a list of patterns for one overview field, most preferred
// first. Each pattern must match the whole original label.
type labelSet []*regexp.Regexp

func labels(patterns ...string) labelSet {
	set := make(labelSet, len(patterns))
	for i, p := range patterns {
		set[i] = regexp.MustCompile(`(?i)^(?:` + p + `)$`)
	}
	return set
}

// anyLabel matches every label; the reading type alone decides.
var anyLabel = labels(`.*`)

// cpuSensor and gpuSensor recognise the sensors HWiNFO creates per processor
// and per graphics adapter, and capture the device they belong to. One device
// usually has several sensors that differ only in the suffix:
//
//	CPU [#0]: AMD Ryzen 9 9950X3D
//	CPU [#0]: AMD Ryzen 9 9950X3D: C-State Residency
//	CPU [#0]: AMD Ryzen 9 9950X3D: Enhanced
//	dGPU [#0]: NVIDIA GeForce RTX 5080
//
// HWiNFO 8 prefixes graphics adapters with their kind ("dGPU", "iGPU",
// "eGPU"); older versions wrote a plain "GPU".
var (
	cpuSensor = regexp.MustCompile(`(?i)^CPU \[#\d+\]:\s*([^:]+)`)
	gpuSensor = regexp.MustCompile(`(?i)^[die]?GPU \[#\d+\]:\s*([^:]+)`)
)

// cpuLabel holds the original labels behind each CPU field.
//
// The first pattern of every set is what a Ryzen 9 9950X3D reports under
// HWiNFO 8.60 and is covered by the regression test built from that machine.
// The later ones are HWiNFO's names on older AMD and on Intel processors.
var cpuLabel = struct {
	temperature  labelSet // the temperature the processor regulates itself by
	dieAverage   labelSet
	ccd          labelSet // one per core complex die
	core         labelSet // one per core
	packagePower labelSet
	thermalLimit labelSet // percentage of the temperature limit in use
	throttling   labelSet // Yes/No flags
}{
	temperature:  labels(`CPU \(Tctl/Tdie\)`, `CPU \(Tdie\)`, `CPU \(Tctl\)`, `CPU Package`, `Core Max`),
	dieAverage:   labels(`CPU Die \(average\)`),
	ccd:          labels(`CPU CCD\d+ \(Tdie\)`),
	core:         labels(`Core ?\d+ \(CCD\d+\)`, `(?:P-core|E-core|Core) ?\d+`),
	packagePower: labels(`CPU Package Power`, `CPU PPT`),
	thermalLimit: labels(`Thermal Limit`),
	throttling:   labels(`.*Thermal Throttling.*`),
}

// gpuLabel holds the original labels behind each GPU field. As with cpuLabel,
// the first pattern of every set is verified against an RTX 5080.
//
// "GPU Power" is the draw of the whole board on that card. It must match
// exactly: the same sensor also has "GPU Power Limit (rated)" and a dozen
// per-rail readings such as "GPU 12VHPWR Power", all of them of type power.
// The later power patterns are a mix: "Total Board Power (TBP)" is the board
// again, while "GPU PPT" and "GPU ASIC Power" are the chip alone, and an
// integrated adapter has no board at all. That is why the field is called
// Power and not BoardPower: the label it carries says what was measured.
var gpuLabel = struct {
	temperature    labelSet
	hotSpot        labelSet
	memoryJunction labelSet
	power          labelSet
	throttling     labelSet // Yes/No flags
}{
	temperature:    labels(`GPU Temperature`),
	hotSpot:        labels(`GPU Hot ?Spot Temperature`),
	memoryJunction: labels(`GPU Memory Junction Temperature`, `GPU Memory Temperature`),
	power:          labels(`GPU Power`, `Total Board Power \(TBP\)`, `GPU PPT`, `GPU ASIC Power`),
	throttling:     labels(`Performance Limit - Thermal`),
}

// Thermals is the curated view produced by Snapshot.Thermals.
type Thermals struct {
	CPUs []CPUThermals
	GPUs []GPUThermals

	// Other holds the temperature readings of every sensor that is neither
	// a processor nor a graphics adapter: mainboard, memory, drives.
	Other []Reading

	// Fans holds the fan speeds that are not attributed to a graphics card.
	Fans []Reading
}

// CPUThermals describes one processor. A nil or empty field means HWiNFO
// offers no matching reading.
type CPUThermals struct {
	Name string // for example "AMD Ryzen 9 9950X3D"

	Temperature  *Reading  // Tctl/Tdie on AMD, package temperature on Intel
	DieAverage   *Reading  // AMD only
	CCDs         []Reading // AMD only, one per core complex die
	HottestCore  *Reading  // the per-core temperature that is highest right now
	PackagePower *Reading

	// ThermalLimit is how much of its temperature limit the processor uses,
	// in percent (AMD). At 100 the processor holds itself at the limit, and
	// it does so without setting any of the flags behind Throttling: on the
	// machine the test fixture comes from, this reading peaked at 100 while
	// every flag stayed clear.
	ThermalLimit *Reading

	Throttling *Throttling
}

// GPUThermals describes one graphics adapter.
type GPUThermals struct {
	Name string // for example "NVIDIA GeForce RTX 5080"

	Temperature    *Reading
	HotSpot        *Reading
	MemoryJunction *Reading
	Power          *Reading // the whole board where HWiNFO reports it, else the chip
	Fans           []Reading

	Throttling *Throttling
}

// Throttling summarises HWiNFO's Yes/No thermal flags for a device. Clear
// flags do not mean the device was never limited by temperature: see
// CPUThermals.ThermalLimit.
type Throttling struct {
	Active   bool // a flag is set right now
	Occurred bool // a flag has been set at some point since HWiNFO started
}

// A device is the set of sensors that belong to one processor or adapter.
type device struct {
	name    string
	sensors map[int]bool
}

// devices groups the sensors matched by re by the device they name, in the
// order HWiNFO lists them.
func (s *Snapshot) devices(re *regexp.Regexp) []device {
	var devs []device
	byKey := map[string]int{}
	for _, sn := range s.Sensors {
		m := re.FindStringSubmatch(sn.OriginalName)
		if m == nil {
			continue
		}
		key := fold(m[0]) // prefix, index and model: "cpu [#0]: amd ryzen 9 9950x3d"
		i, ok := byKey[key]
		if !ok {
			i = len(devs)
			byKey[key] = i
			devs = append(devs, device{name: strings.TrimSpace(m[1]), sensors: map[int]bool{}})
		}
		devs[i].sensors[sn.Index] = true
	}
	return devs
}

// first returns the device's reading of type t for the most preferred pattern
// in set that matches anything.
func (d device) first(s *Snapshot, t ReadingType, set labelSet) *Reading {
	for _, re := range set {
		for i := range s.Readings {
			r := &s.Readings[i]
			if r.Type == t && d.sensors[r.SensorIndex] && re.MatchString(r.OriginalLabel) {
				return r
			}
		}
	}
	return nil
}

// all returns every reading of type t of the device that matches any pattern
// in set, in HWiNFO's order.
func (d device) all(s *Snapshot, t ReadingType, set labelSet) []Reading {
	var out []Reading
	for _, r := range s.Readings {
		if r.Type != t || !d.sensors[r.SensorIndex] {
			continue
		}
		for _, re := range set {
			if re.MatchString(r.OriginalLabel) {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

// hottest returns the reading with the highest current value, or nil.
// Readings that hold no number are passed over.
func hottest(rs []Reading) *Reading {
	var best *Reading
	for i := range rs {
		if math.IsNaN(rs[i].Value) {
			continue
		}
		if best == nil || rs[i].Value > best.Value {
			best = &rs[i]
		}
	}
	return best
}

// throttling folds a device's Yes/No thermal flags into one summary, or
// returns nil if the device has none.
func (d device) throttling(s *Snapshot, set labelSet) *Throttling {
	flags := d.all(s, TypeOther, set)
	if len(flags) == 0 {
		return nil
	}
	var t Throttling
	for _, f := range flags {
		t.Active = t.Active || f.Value != 0
		t.Occurred = t.Occurred || f.Max != 0
	}
	return &t
}

// Thermals picks the readings that describe how hot the system runs.
func (s *Snapshot) Thermals() Thermals {
	var th Thermals
	claimed := map[int]bool{} // sensors that belong to a CPU or GPU

	for _, d := range s.devices(cpuSensor) {
		for i := range d.sensors {
			claimed[i] = true
		}
		th.CPUs = append(th.CPUs, CPUThermals{
			Name:         d.name,
			Temperature:  d.first(s, TypeTemperature, cpuLabel.temperature),
			DieAverage:   d.first(s, TypeTemperature, cpuLabel.dieAverage),
			CCDs:         d.all(s, TypeTemperature, cpuLabel.ccd),
			HottestCore:  hottest(d.all(s, TypeTemperature, cpuLabel.core)),
			PackagePower: d.first(s, TypePower, cpuLabel.packagePower),
			ThermalLimit: d.first(s, TypeUsage, cpuLabel.thermalLimit),
			Throttling:   d.throttling(s, cpuLabel.throttling),
		})
	}

	gpuFans := map[int]bool{}
	for _, d := range s.devices(gpuSensor) {
		for i := range d.sensors {
			claimed[i] = true
			gpuFans[i] = true
		}
		th.GPUs = append(th.GPUs, GPUThermals{
			Name:           d.name,
			Temperature:    d.first(s, TypeTemperature, gpuLabel.temperature),
			HotSpot:        d.first(s, TypeTemperature, gpuLabel.hotSpot),
			MemoryJunction: d.first(s, TypeTemperature, gpuLabel.memoryJunction),
			Power:          d.first(s, TypePower, gpuLabel.power),
			Fans:           d.all(s, TypeFan, anyLabel),
			Throttling:     d.throttling(s, gpuLabel.throttling),
		})
	}

	for _, r := range s.Readings {
		switch {
		case r.Type == TypeTemperature && !claimed[r.SensorIndex]:
			th.Other = append(th.Other, r)
		case r.Type == TypeFan && !gpuFans[r.SensorIndex]:
			th.Fans = append(th.Fans, r)
		}
	}
	return th
}
