// Package hwinfotest builds synthetic HWiNFO shared memory blocks for tests.
//
// It encodes the layout on its own and deliberately does not import package
// hwinfo: a wrong offset in the parser then shows up as a failing test
// instead of being mirrored by the fixture.
package hwinfotest

import (
	"encoding/binary"
	"math"
)

// Reading types, as HWiNFO numbers them.
const (
	None uint32 = iota
	Temperature
	Voltage
	Fan
	Current
	Power
	Clock
	Usage
	Other
)

// A Sensor describes one sensor element.
type Sensor struct {
	ID       uint32
	Instance uint32
	Name     string // HWiNFO's original name

	// UserName is the name HWiNFO displays. Empty means the same as Name,
	// which is what HWiNFO writes for a sensor that is neither translated
	// nor renamed.
	UserName string
}

// A Reading describes one reading element.
type Reading struct {
	Type      uint32
	Sensor    uint32 // index into Image.Sensors
	ID        uint32
	Label     string // HWiNFO's original label
	UserLabel string // displayed label; empty means the same as Label
	Unit      string
	Value     float64
	Min       float64
	Max       float64
	Avg       float64
}

// An Image describes a complete shared memory block.
type Image struct {
	// Layout selects the generation of the format:
	//
	//	1  44-byte header, 264-byte sensors, 316-byte readings, ANSI text only
	//	2  48-byte header with the polling period, 392-byte sensors and
	//	   460-byte readings that add UTF-8 copies of the user-facing text
	//
	// Zero means 2, which is what current HWiNFO versions write.
	Layout int

	Dead          bool   // write the signature of a block HWiNFO stopped updating
	PollTime      int64  // Unix seconds
	PollingPeriod uint32 // milliseconds; layout 2 only

	Sensors  []Sensor
	Readings []Reading

	// SensorPad and ReadingPad grow every element by that many zero bytes,
	// the way a future HWiNFO that appends fields would.
	SensorPad  int
	ReadingPad int

	// Tail appends zero bytes after the last element, like the unused
	// remainder of the real mapping.
	Tail int
}

const (
	nameLen = 128
	unitLen = 16
)

// Sizes returns the header, sensor element and reading element sizes of im.
func (im Image) Sizes() (header, sensor, reading int) {
	if im.Layout == 1 {
		return 44, 264 + im.SensorPad, 316 + im.ReadingPad
	}
	return 48, 392 + im.SensorPad, 460 + im.ReadingPad
}

// SensorOffset returns the offset of sensor element i within the block.
func (im Image) SensorOffset(i int) int {
	header, sensor, _ := im.Sizes()
	return header + i*sensor
}

// ReadingOffset returns the offset of reading element i within the block.
func (im Image) ReadingOffset(i int) int {
	header, sensor, reading := im.Sizes()
	return header + len(im.Sensors)*sensor + i*reading
}

// Bytes encodes the block exactly as HWiNFO lays it out in memory.
func (im Image) Bytes() []byte {
	header, sensorSize, readingSize := im.Sizes()
	v2 := im.Layout != 1
	le := binary.LittleEndian

	b := make([]byte, header+len(im.Sensors)*sensorSize+len(im.Readings)*readingSize+im.Tail)

	if im.Dead {
		copy(b, "DAED")
	} else {
		copy(b, "HWiS")
	}
	if v2 {
		le.PutUint32(b[4:], 2) // version
		le.PutUint32(b[8:], 1) // revision
		le.PutUint32(b[44:], im.PollingPeriod)
	} else {
		le.PutUint32(b[4:], 1)
	}
	le.PutUint64(b[12:], uint64(im.PollTime))
	le.PutUint32(b[20:], uint32(header))
	le.PutUint32(b[24:], uint32(sensorSize))
	le.PutUint32(b[28:], uint32(len(im.Sensors)))
	le.PutUint32(b[32:], uint32(header+len(im.Sensors)*sensorSize))
	le.PutUint32(b[36:], uint32(readingSize))
	le.PutUint32(b[40:], uint32(len(im.Readings)))

	for i, s := range im.Sensors {
		e := b[im.SensorOffset(i):]
		user := s.UserName
		if user == "" {
			user = s.Name
		}
		le.PutUint32(e[0:], s.ID)
		le.PutUint32(e[4:], s.Instance)
		putANSI(e[8:8+nameLen], s.Name)
		putANSI(e[136:136+nameLen], user)
		if v2 {
			putUTF8(e[264:264+nameLen], user)
		}
	}

	for i, r := range im.Readings {
		e := b[im.ReadingOffset(i):]
		user := r.UserLabel
		if user == "" {
			user = r.Label
		}
		le.PutUint32(e[0:], r.Type)
		le.PutUint32(e[4:], r.Sensor)
		le.PutUint32(e[8:], r.ID)
		putANSI(e[12:12+nameLen], r.Label)
		putANSI(e[140:140+nameLen], user)
		putANSI(e[268:268+unitLen], r.Unit)
		le.PutUint64(e[284:], math.Float64bits(r.Value))
		le.PutUint64(e[292:], math.Float64bits(r.Min))
		le.PutUint64(e[300:], math.Float64bits(r.Max))
		le.PutUint64(e[308:], math.Float64bits(r.Avg))
		if v2 {
			putUTF8(e[316:316+nameLen], user)
			putUTF8(e[444:444+unitLen], r.Unit)
		}
	}
	return b
}

// putANSI writes s as a NUL-terminated Windows-1252 string. Characters the
// code page cannot express become '?', as they do when Windows converts.
func putANSI(field []byte, s string) {
	i := 0
	for _, r := range s {
		if i >= len(field)-1 {
			break
		}
		switch {
		case r < 0x80, r >= 0xA0 && r <= 0xFF:
			field[i] = byte(r)
		default:
			field[i] = '?'
		}
		i++
	}
}

// putUTF8 writes s as a NUL-terminated UTF-8 string, cut at a character
// boundary if it does not fit.
func putUTF8(field []byte, s string) {
	limit := len(field) - 1 // leave room for the terminator
	if len(s) > limit {
		cut := 0
		for i := range s { // i walks the start of every character
			if i > limit {
				break
			}
			cut = i
		}
		s = s[:cut]
	}
	copy(field, s)
}
