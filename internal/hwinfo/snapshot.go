package hwinfo

import (
	"errors"
	"fmt"
	"time"
)

var (
	// ErrInactive reports that the block carries the "dead" signature: HWiNFO
	// stopped publishing (the Sensors window was closed, or the free edition
	// reached its 12-hour shared-memory limit). Parse returns it together
	// with the last snapshot HWiNFO wrote, whose values are stale.
	ErrInactive = errors.New("hwinfo: HWiNFO stopped publishing sensor data")

	// ErrSignature reports a block that does not start with a HWiNFO signature.
	ErrSignature = errors.New("hwinfo: not a HWiNFO shared memory block")

	// ErrLayout reports a block whose header describes data that cannot be
	// read safely: truncated, overlapping the header, or with undersized elements.
	ErrLayout = errors.New("hwinfo: inconsistent shared memory layout")

	// ErrNotFound reports that the shared memory mapping does not exist.
	ErrNotFound = errors.New("hwinfo: shared memory not found")

	// ErrAccessDenied reports that the mapping exists but may not be opened.
	ErrAccessDenied = errors.New("hwinfo: access to shared memory denied")

	// ErrUnsupported is returned by Reader on systems other than Windows.
	ErrUnsupported = errors.New("hwinfo: HWiNFO shared memory is only available on Windows")
)

// ReadingType is the kind of quantity a reading measures.
type ReadingType uint32

// Reading types, in the order HWiNFO numbers them.
const (
	TypeNone ReadingType = iota
	TypeTemperature
	TypeVoltage
	TypeFan
	TypeCurrent
	TypePower
	TypeClock
	TypeUsage
	TypeOther
)

var typeNames = [...]string{
	TypeNone:        "none",
	TypeTemperature: "temperature",
	TypeVoltage:     "voltage",
	TypeFan:         "fan",
	TypeCurrent:     "current",
	TypePower:       "power",
	TypeClock:       "clock",
	TypeUsage:       "usage",
	TypeOther:       "other",
}

// String returns the lower-case name of the type, such as "temperature".
func (t ReadingType) String() string {
	if int(t) < len(typeNames) {
		return typeNames[t]
	}
	return fmt.Sprintf("type%d", uint32(t))
}

// TypeNames lists the names accepted by ParseReadingType, without "none".
func TypeNames() []string {
	return append([]string(nil), typeNames[TypeTemperature:]...)
}

// ParseReadingType converts a name produced by ReadingType.String back to
// the type. A few common short forms ("temp", "volt") are accepted too.
func ParseReadingType(name string) (ReadingType, bool) {
	switch fold(name) {
	case "temp", "temps", "temperatures":
		return TypeTemperature, true
	case "volt", "volts", "voltages":
		return TypeVoltage, true
	case "fans":
		return TypeFan, true
	case "currents":
		return TypeCurrent, true
	case "powers":
		return TypePower, true
	case "clocks", "frequency":
		return TypeClock, true
	case "load":
		return TypeUsage, true
	}
	for t, n := range typeNames {
		if n == fold(name) {
			return ReadingType(t), true
		}
	}
	return 0, false
}

// A Sensor is a group of readings that come from one device or chip.
type Sensor struct {
	Index    int // position in Snapshot.Sensors
	ID       uint32
	Instance uint32

	// OriginalName is HWiNFO's own name for the sensor. It is always
	// English and does not change when the user renames the sensor.
	OriginalName string

	// Name is what the Sensors window shows: OriginalName translated to
	// the language HWiNFO runs in, or the name the user gave the sensor.
	Name string
}

// A Reading is one measured value with the statistics HWiNFO keeps for it.
type Reading struct {
	Index       int // position in Snapshot.Readings
	Type        ReadingType
	SensorIndex int // index into Snapshot.Sensors, or -1 if HWiNFO gave none
	ID          uint32

	// OriginalLabel is HWiNFO's own English label; Label is what the
	// Sensors window shows (translated or renamed). See Sensor.
	OriginalLabel string
	Label         string

	Unit string // for example "°C", "W" or "RPM"; may be empty

	Value float64
	Min   float64 // lowest value since HWiNFO started monitoring
	Max   float64 // highest value since HWiNFO started monitoring
	Avg   float64
}

// A Snapshot is a consistent copy of everything HWiNFO published at one poll.
type Snapshot struct {
	// Active is false when HWiNFO has stopped publishing; the rest of the
	// snapshot is then the last state it wrote.
	Active bool

	// Locked reports whether the copy was taken while holding HWiNFO's
	// mutex. Parse leaves it false; Reader.Snapshot sets it.
	Locked bool

	Version  uint32
	Revision uint32

	// PollTime is when HWiNFO last polled its sensors. It is the zero time
	// if the block holds no plausible timestamp.
	PollTime time.Time

	// PollingPeriod is HWiNFO's sensor polling interval, or zero when the
	// block does not say (version 1 layouts).
	PollingPeriod time.Duration

	Sensors  []Sensor
	Readings []Reading
}

// Sensor returns the sensor with the given index, or nil if there is none.
func (s *Snapshot) Sensor(index int) *Sensor {
	if index < 0 || index >= len(s.Sensors) {
		return nil
	}
	return &s.Sensors[index]
}

// maxPollTime is 9999-12-31T23:59:59Z, the last instant RFC 3339 can express.
const maxPollTime = 253402300799

// Parse decodes a copy of the HWiNFO shared memory block.
//
// Every offset and size in the header is checked against len(data) before
// anything is read, so Parse is safe on truncated or corrupted input.
//
// When the block carries the "dead" signature, Parse returns the stale
// snapshot together with ErrInactive, so that callers can still show the last
// known values. Any other error comes with a nil snapshot.
func Parse(data []byte) (*Snapshot, error) {
	h, err := parseHeader(data)
	if err != nil {
		return nil, err
	}
	snap := &Snapshot{
		Active:        h.active,
		Version:       h.version,
		Revision:      h.revision,
		PollingPeriod: time.Duration(h.pollingPeriod) * time.Millisecond,
	}
	if h.pollTime > 0 && h.pollTime <= maxPollTime {
		snap.PollTime = time.Unix(h.pollTime, 0)
	}
	if err := h.validate(uint64(len(data))); err != nil {
		if !h.active {
			// The contents of a dead block are of no interest beyond the
			// fact that it is dead.
			return snap, ErrInactive
		}
		return nil, err
	}

	snap.Sensors = make([]Sensor, h.sensorCount)
	for i := range snap.Sensors {
		off := int(h.sensorOffset) + i*int(h.sensorSize)
		snap.Sensors[i] = parseSensor(data[off:off+int(h.sensorSize)], i)
	}
	snap.Readings = make([]Reading, h.readingCount)
	for i := range snap.Readings {
		off := int(h.readingOffset) + i*int(h.readingSize)
		snap.Readings[i] = parseReading(data[off:off+int(h.readingSize)], i, len(snap.Sensors))
	}

	if !h.active {
		return snap, ErrInactive
	}
	return snap, nil
}

// blockSize returns how many leading bytes of view make up the block: the
// header plus both sections. view is the mapped memory, which is usually much
// larger than the part HWiNFO uses.
//
// For a dead block with a nonsensical layout only the header is reported, so
// that the caller still copies enough for Parse to recognise it as inactive.
func blockSize(view []byte) (int, error) {
	h, err := parseHeader(view)
	if err != nil {
		return 0, err
	}
	if err := h.validate(uint64(len(view))); err != nil {
		if !h.active {
			return h.size, nil
		}
		return 0, err
	}
	return int(h.extent()), nil
}

// A Reader copies the shared memory block that HWiNFO publishes. The zero
// value reads HWiNFO's own objects; the fields exist so that tests can point
// it at objects of their own.
type Reader struct {
	Mapping  string        // name of the file mapping; MappingName if empty
	Mutex    string        // name of the mutex guarding it; MutexName if empty
	LockWait time.Duration // how long to wait for the mutex; 200ms if zero
}

func (r Reader) withDefaults() Reader {
	if r.Mapping == "" {
		r.Mapping = MappingName
	}
	if r.Mutex == "" {
		r.Mutex = MutexName
	}
	if r.LockWait <= 0 {
		r.LockWait = 200 * time.Millisecond
	}
	return r
}

// Snapshot reads and decodes the current state of HWiNFO's sensors.
//
// Like Parse, it returns a stale snapshot together with ErrInactive when
// HWiNFO has stopped publishing.
func (r Reader) Snapshot() (*Snapshot, error) {
	data, locked, err := r.Read()
	if err != nil {
		return nil, err
	}
	snap, err := Parse(data)
	if snap != nil {
		snap.Locked = locked
	}
	return snap, err
}
