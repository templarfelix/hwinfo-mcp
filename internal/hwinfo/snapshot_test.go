package hwinfo

import (
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/templarfelix/hwinfo-mcp/internal/hwinfo/hwinfotest"
)

// degC is the unit as it must come out of the parser. It is spelled with an
// escape so that the test does not depend on how this file is encoded.
const degC = "°C"

// sample is a small block that exercises every field: two sensors, one of
// them translated, and readings with translated labels and non-ASCII units.
func sample(layout int) hwinfotest.Image {
	return hwinfotest.Image{
		Layout:        layout,
		PollTime:      1795462303,
		PollingPeriod: 2000,
		Sensors: []hwinfotest.Sensor{
			{ID: 0xF0000501, Instance: 0, Name: "CPU [#0]: AMD Ryzen 9 9950X3D: Enhanced"},
			{ID: 0xF0000301, Instance: 1, Name: "Memory Timings", UserName: "Tempos da memória"},
		},
		Readings: []hwinfotest.Reading{
			{Type: hwinfotest.Temperature, Sensor: 0, ID: 0x01000000, Label: "CPU (Tctl/Tdie)", Unit: degC, Value: 73, Min: 60.5, Max: 80.125, Avg: 71.25},
			{Type: hwinfotest.Power, Sensor: 0, ID: 0x05000000, Label: "CPU Package Power", UserLabel: "Potência total da CPU", Unit: "W", Value: 166.3, Min: 40, Max: 190, Avg: 120},
			{Type: hwinfotest.Clock, Sensor: 1, ID: 0x06000000, Label: "Memory Clock", UserLabel: "Relógio da memória", Unit: "MHz", Value: 3194.24, Min: 3194.24, Max: 3194.24, Avg: 3194.24},
			{Type: hwinfotest.Other, Sensor: 1, ID: 0x08000000, Label: "Tcas", Unit: "T", Value: 32, Min: 32, Max: 32, Avg: 32},
		},
	}
}

func TestParse(t *testing.T) {
	for _, layout := range []int{1, 2} {
		im := sample(layout)
		snap, err := Parse(im.Bytes())
		if err != nil {
			t.Fatalf("layout %d: Parse: %v", layout, err)
		}

		if !snap.Active {
			t.Errorf("layout %d: Active = false, want true", layout)
		}
		if snap.Locked {
			t.Errorf("layout %d: Parse set Locked; only a Reader may", layout)
		}
		if got := snap.PollTime.Unix(); got != im.PollTime {
			t.Errorf("layout %d: PollTime = %d, want %d", layout, got, im.PollTime)
		}
		wantVersion, wantPeriod := uint32(2), 2*time.Second
		if layout == 1 {
			wantVersion, wantPeriod = 1, 0 // version 1 has no polling period
		}
		if snap.Version != wantVersion || snap.PollingPeriod != wantPeriod {
			t.Errorf("layout %d: Version %d, PollingPeriod %v; want %d, %v", layout, snap.Version, snap.PollingPeriod, wantVersion, wantPeriod)
		}

		wantSensors := []Sensor{
			{Index: 0, ID: 0xF0000501, Instance: 0, OriginalName: "CPU [#0]: AMD Ryzen 9 9950X3D: Enhanced", Name: "CPU [#0]: AMD Ryzen 9 9950X3D: Enhanced"},
			{Index: 1, ID: 0xF0000301, Instance: 1, OriginalName: "Memory Timings", Name: "Tempos da memória"},
		}
		if len(snap.Sensors) != len(wantSensors) {
			t.Fatalf("layout %d: %d sensors, want %d", layout, len(snap.Sensors), len(wantSensors))
		}
		for i, want := range wantSensors {
			if snap.Sensors[i] != want {
				t.Errorf("layout %d: sensor %d = %+v, want %+v", layout, i, snap.Sensors[i], want)
			}
		}

		wantReadings := []Reading{
			{Index: 0, Type: TypeTemperature, SensorIndex: 0, ID: 0x01000000, OriginalLabel: "CPU (Tctl/Tdie)", Label: "CPU (Tctl/Tdie)", Unit: degC, Value: 73, Min: 60.5, Max: 80.125, Avg: 71.25},
			{Index: 1, Type: TypePower, SensorIndex: 0, ID: 0x05000000, OriginalLabel: "CPU Package Power", Label: "Potência total da CPU", Unit: "W", Value: 166.3, Min: 40, Max: 190, Avg: 120},
			{Index: 2, Type: TypeClock, SensorIndex: 1, ID: 0x06000000, OriginalLabel: "Memory Clock", Label: "Relógio da memória", Unit: "MHz", Value: 3194.24, Min: 3194.24, Max: 3194.24, Avg: 3194.24},
			{Index: 3, Type: TypeOther, SensorIndex: 1, ID: 0x08000000, OriginalLabel: "Tcas", Label: "Tcas", Unit: "T", Value: 32, Min: 32, Max: 32, Avg: 32},
		}
		if len(snap.Readings) != len(wantReadings) {
			t.Fatalf("layout %d: %d readings, want %d", layout, len(snap.Readings), len(wantReadings))
		}
		for i, want := range wantReadings {
			if snap.Readings[i] != want {
				t.Errorf("layout %d: reading %d = %+v, want %+v", layout, i, snap.Readings[i], want)
			}
		}
	}
}

// The degree sign is the one non-ASCII character every HWiNFO installation
// produces. Layout 1 stores it as the single Windows-1252 byte B0; layout 2
// adds a UTF-8 copy (C2 B0). Both must decode to the same string.
func TestParseDegreeSign(t *testing.T) {
	for _, layout := range []int{1, 2} {
		im := sample(layout)
		raw := im.Bytes()

		unit := raw[im.ReadingOffset(0)+268:]
		if unit[0] != 0xB0 || unit[1] != 'C' || unit[2] != 0 {
			t.Fatalf("layout %d: fixture wrote ANSI unit % X, want B0 43 00", layout, unit[:3])
		}
		if layout == 2 {
			utf := raw[im.ReadingOffset(0)+444:]
			if utf[0] != 0xC2 || utf[1] != 0xB0 || utf[2] != 'C' || utf[3] != 0 {
				t.Fatalf("fixture wrote UTF-8 unit % X, want C2 B0 43 00", utf[:4])
			}
		}

		snap, err := Parse(raw)
		if err != nil {
			t.Fatalf("layout %d: Parse: %v", layout, err)
		}
		if got := snap.Readings[0].Unit; got != degC {
			t.Errorf("layout %d: unit = %q (% X), want %q", layout, got, got, degC)
		}
	}
}

// A layout-2 block whose UTF-8 copy is damaged must fall back to the ANSI
// copy rather than hand out invalid UTF-8.
func TestParseInvalidUTF8FallsBackToANSI(t *testing.T) {
	im := sample(2)
	raw := im.Bytes()
	copy(raw[im.ReadingOffset(1)+316:], []byte{0xFF, 0xFE, 'x', 0})
	copy(raw[im.ReadingOffset(0)+444:], []byte{0xB0, 'C', 0, 0}) // a lone B0 is not UTF-8

	snap, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := snap.Readings[1].Label, "Potência total da CPU"; got != want {
		t.Errorf("label = %q, want the ANSI copy %q", got, want)
	}
	if got := snap.Readings[0].Unit; got != degC {
		t.Errorf("unit = %q, want %q", got, degC)
	}
}

// HWiNFO cuts a name that is too long for its field wherever the field ends,
// even inside a multi-byte character. Only that last character is lost; the
// rest of the UTF-8 text is still used.
func TestParseNameCutInsideACharacter(t *testing.T) {
	im := sample(2)
	raw := im.Bytes()
	field := raw[im.ReadingOffset(1)+316 : im.ReadingOffset(1)+316+128]
	long := []byte("Potência " + strings.Repeat("x", 116)) // 126 bytes
	copy(field, long)
	field[126] = 0xC3 // first half of a two-byte character, cut by the field's end
	field[127] = 0

	snap, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := snap.Readings[1].Label; got != string(long) {
		t.Errorf("label = %q, want the UTF-8 text without the cut-off character", got)
	}

	tests := []struct{ in, want string }{
		{"", ""},
		{"abc", "abc"},
		{"café", "café"},
		{"caf\xc3", "caf"},     // two-byte character, one byte present
		{"x\xe2\x82", "x"},     // three-byte character, two present
		{"x\xf0\x9f\x98", "x"}, // four-byte character, three present
		{"\xe6\xb8\xa9", "温"},  // complete three-byte character
		{"a\xffb", "a\xffb"},   // invalid in the middle: not this function's business
		{"\xc3", ""},
	}
	for _, tt := range tests {
		if got := string(trimPartialRune([]byte(tt.in))); got != tt.want {
			t.Errorf("trimPartialRune(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParseInactive(t *testing.T) {
	for _, sig := range []string{"DAED", "DEAD"} {
		im := sample(2)
		im.Dead = true
		raw := im.Bytes()
		copy(raw, sig)

		snap, err := Parse(raw)
		if !errors.Is(err, ErrInactive) {
			t.Fatalf("%s: err = %v, want ErrInactive", sig, err)
		}
		if snap == nil {
			t.Fatalf("%s: no snapshot; the stale data must still be returned", sig)
		}
		if snap.Active {
			t.Errorf("%s: Active = true", sig)
		}
		if len(snap.Sensors) != 2 || len(snap.Readings) != 4 || snap.Readings[0].Value != 73 {
			t.Errorf("%s: stale data lost: %d sensors, %d readings", sig, len(snap.Sensors), len(snap.Readings))
		}
		if snap.PollTime.Unix() != im.PollTime {
			t.Errorf("%s: PollTime = %v, want the time of the last poll", sig, snap.PollTime)
		}
	}
}

// A dead block is reported as inactive even if what is left in it no longer
// makes sense; there is nothing better to say about it.
func TestParseInactiveWithBrokenLayout(t *testing.T) {
	im := sample(2)
	im.Dead = true
	raw := im.Bytes()
	binary.LittleEndian.PutUint32(raw[40:], 1<<30) // reading count

	snap, err := Parse(raw)
	if !errors.Is(err, ErrInactive) {
		t.Fatalf("err = %v, want ErrInactive", err)
	}
	if snap == nil || snap.Active || len(snap.Readings) != 0 {
		t.Errorf("snapshot = %+v, want an inactive one without readings", snap)
	}
}

func TestParseRejectsBadBlocks(t *testing.T) {
	le := binary.LittleEndian
	good := sample(2).Bytes()
	patch := func(offset int, value uint32) []byte {
		b := append([]byte(nil), good...)
		le.PutUint32(b[offset:], value)
		return b
	}

	tests := []struct {
		name string
		data []byte
		want error
	}{
		{"empty", nil, ErrLayout},
		{"shorter than a header", good[:43], ErrLayout},
		{"version 2 header cut before the polling period", good[:47], ErrLayout},
		{"not HWiNFO", append([]byte("MZ\x90\x00"), good[4:]...), ErrSignature},
		{"zeroed", make([]byte, len(good)), ErrSignature},
		{"last reading cut short", good[:len(good)-1], ErrLayout},
		{"sensor element too small", patch(24, 263), ErrLayout},
		{"reading element too small", patch(36, 315), ErrLayout},
		{"sensor section inside the header", patch(20, 40), ErrLayout},
		{"reading section inside the header", patch(32, 0), ErrLayout},
		{"sensor count past the end", patch(28, 7), ErrLayout},
		{"reading count past the end", patch(40, 5), ErrLayout},
		{"count that overflows 32 bits", patch(40, math.MaxUint32), ErrLayout},
		{"offset that overflows 32 bits", patch(32, math.MaxUint32), ErrLayout},
		{"element size that overflows 32 bits", patch(36, math.MaxUint32), ErrLayout},
	}
	for _, tt := range tests {
		snap, err := Parse(tt.data)
		if !errors.Is(err, tt.want) {
			t.Errorf("%s: err = %v, want %v", tt.name, err, tt.want)
		}
		if snap != nil {
			t.Errorf("%s: got a snapshot alongside the error", tt.name)
		}
	}
}

// Cutting the block at every possible length must produce an error, never a
// panic or an out-of-range read.
func TestParseEveryTruncation(t *testing.T) {
	for _, layout := range []int{1, 2} {
		for _, dead := range []bool{false, true} {
			im := sample(layout)
			im.Dead = dead
			raw := im.Bytes()
			for n := 0; n < len(raw); n++ {
				snap, err := Parse(raw[:n:n])
				if err == nil {
					t.Fatalf("layout %d dead=%v: %d of %d bytes parsed without error", layout, dead, n, len(raw))
				}
				if snap != nil && !errors.Is(err, ErrInactive) {
					t.Fatalf("layout %d dead=%v: snapshot returned with %v", layout, dead, err)
				}
			}
		}
	}
}

// Elements may grow in future HWiNFO versions. The sizes in the header say
// how far apart they are; the known fields stay where they were.
func TestParseLargerElements(t *testing.T) {
	im := sample(2)
	im.SensorPad, im.ReadingPad = 24, 40
	snap, err := Parse(im.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Sensors) != 2 || snap.Sensors[1].Name != "Tempos da memória" {
		t.Errorf("sensors misread: %+v", snap.Sensors)
	}
	if len(snap.Readings) != 4 || snap.Readings[3].OriginalLabel != "Tcas" || snap.Readings[3].Value != 32 {
		t.Errorf("readings misread: %+v", snap.Readings)
	}
}

func TestParseEmptyBlock(t *testing.T) {
	snap, err := Parse(hwinfotest.Image{PollTime: 1795462303}.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Active || len(snap.Sensors) != 0 || len(snap.Readings) != 0 {
		t.Errorf("snapshot = %+v, want an active one with nothing in it", snap)
	}
}

func TestParseReadingWithUnknownSensor(t *testing.T) {
	im := sample(2)
	im.Readings[2].Sensor = 2 // one past the last sensor
	im.Readings[3].Sensor = math.MaxUint32
	snap, err := Parse(im.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	for _, i := range []int{2, 3} {
		if got := snap.Readings[i].SensorIndex; got != -1 {
			t.Errorf("reading %d: SensorIndex = %d, want -1", i, got)
		}
	}
	if snap.Sensor(-1) != nil || snap.Sensor(2) != nil || snap.Sensor(1) == nil {
		t.Error("Snapshot.Sensor does not guard its index")
	}
}

func TestParseImplausiblePollTime(t *testing.T) {
	for _, pt := range []int64{0, -1, math.MaxInt64} {
		im := sample(2)
		im.PollTime = pt
		snap, err := Parse(im.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		if !snap.PollTime.IsZero() {
			t.Errorf("poll time %d became %v, want the zero time", pt, snap.PollTime)
		}
	}
}

func TestBlockSize(t *testing.T) {
	im := sample(2)
	used := len(im.Bytes())
	im.Tail = 4096 // the real mapping is far larger than what HWiNFO fills
	view := im.Bytes()

	if n, err := blockSize(view); err != nil || n != used {
		t.Errorf("blockSize = %d, %v; want %d", n, err, used)
	}

	// The header alone is all that can be said about a dead, broken block.
	copy(view, "DAED")
	binary.LittleEndian.PutUint32(view[40:], 1<<30)
	if n, err := blockSize(view); err != nil || n != headerSizeV2 {
		t.Errorf("dead block: blockSize = %d, %v; want %d", n, err, headerSizeV2)
	}

	copy(view, "HWiS")
	if _, err := blockSize(view); !errors.Is(err, ErrLayout) {
		t.Errorf("broken active block: err = %v, want ErrLayout", err)
	}
	if _, err := blockSize(make([]byte, 64)); !errors.Is(err, ErrSignature) {
		t.Errorf("zeroed view: err = %v, want ErrSignature", err)
	}
}

func TestReadingTypeNames(t *testing.T) {
	want := map[ReadingType]string{
		TypeNone: "none", TypeTemperature: "temperature", TypeVoltage: "voltage",
		TypeFan: "fan", TypeCurrent: "current", TypePower: "power",
		TypeClock: "clock", TypeUsage: "usage", TypeOther: "other",
		ReadingType(42): "type42",
	}
	for typ, name := range want {
		if got := typ.String(); got != name {
			t.Errorf("ReadingType(%d).String() = %q, want %q", typ, got, name)
		}
	}
	for _, name := range TypeNames() {
		typ, ok := ParseReadingType(name)
		if !ok || typ.String() != name {
			t.Errorf("ParseReadingType(%q) = %v, %v", name, typ, ok)
		}
	}
	for name, want := range map[string]ReadingType{"Temp": TypeTemperature, " POWER ": TypePower, "fans": TypeFan, "volt": TypeVoltage} {
		if typ, ok := ParseReadingType(name); !ok || typ != want {
			t.Errorf("ParseReadingType(%q) = %v, %v; want %v", name, typ, ok, want)
		}
	}
	if _, ok := ParseReadingType("pressure"); ok {
		t.Error(`ParseReadingType("pressure") succeeded`)
	}
}

func TestDecodeANSI(t *testing.T) {
	tests := []struct {
		in   []byte
		want string
	}{
		{[]byte("GPU Temperature\x00garbage"), "GPU Temperature"},
		{[]byte{0xB0, 'C', 0}, "°C"},
		{[]byte{'P', 'o', 't', 0xEA, 'n', 'c', 'i', 'a'}, "Potência"},
		{[]byte{0x80, 0x99, 0x85}, "€™…"}, // euro, trade mark, ellipsis
		{nil, ""},
	}
	for _, tt := range tests {
		if got := decodeANSI(tt.in); got != tt.want {
			t.Errorf("decodeANSI(% X) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// FuzzParse checks the property that matters for a parser of foreign memory:
// whatever the bytes, it returns cleanly and its result is self-consistent.
func FuzzParse(f *testing.F) {
	for _, layout := range []int{1, 2} {
		im := sample(layout)
		f.Add(im.Bytes())
		im.Dead = true
		f.Add(im.Bytes())
	}
	f.Add([]byte("HWiS"))
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		if n, err := blockSize(data); err == nil && (n < headerSizeV1 || n > len(data)) {
			t.Fatalf("blockSize = %d for %d bytes", n, len(data))
		}

		snap, err := Parse(data)
		if snap == nil {
			if err == nil {
				t.Fatal("neither a snapshot nor an error")
			}
			return
		}
		if err != nil && !errors.Is(err, ErrInactive) {
			t.Fatalf("snapshot returned together with %v", err)
		}
		if snap.Active != (err == nil) {
			t.Fatalf("Active = %v but err = %v", snap.Active, err)
		}
		for i, s := range snap.Sensors {
			if s.Index != i {
				t.Fatalf("sensor %d has Index %d", i, s.Index)
			}
		}
		for i, r := range snap.Readings {
			if r.Index != i || r.SensorIndex < -1 || r.SensorIndex >= len(snap.Sensors) {
				t.Fatalf("reading %d: Index %d, SensorIndex %d with %d sensors", i, r.Index, r.SensorIndex, len(snap.Sensors))
			}
		}
		// Selecting and summarising must cope with whatever was parsed.
		snap.Select(Filter{Sensor: "cpu", Label: "temp", Types: []ReadingType{TypeTemperature}})
		snap.Thermals()
	})
}
