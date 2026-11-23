// Package hwinfo reads the sensor data that HWiNFO publishes through its
// "Shared Memory Support" interface and picks out the readings that matter.
//
// The package never writes to the shared memory block: it opens the mapping
// read-only, copies it, and works on the copy.
package hwinfo

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"unicode/utf8"
)

// Names of the kernel objects HWiNFO creates while "Shared Memory Support" is
// enabled and the Sensors window is open.
const (
	MappingName = `Global\HWiNFO_SENS_SM2`
	MutexName   = `Global\HWiNFO_SM2_MUTEX`
)

// Layout of the HWiNFO_SENSORS_SHARED_MEM2 block. Every integer is
// little-endian and the structures are packed, so the offsets below are exact.
//
// Header (44 bytes in version 1, 48 bytes since version 2):
//
//	 0  [4]byte  signature: "HWiS" while HWiNFO is publishing, "DAED" once it stops
//	 4  uint32   version
//	 8  uint32   revision
//	12  int64    time of the last poll, in Unix seconds
//	20  uint32   offset of the sensor section
//	24  uint32   size of one sensor element
//	28  uint32   number of sensor elements
//	32  uint32   offset of the reading section
//	36  uint32   size of one reading element
//	40  uint32   number of reading elements
//	44  uint32   polling period in milliseconds (version 2 and later)
//
// Sensor element (264 bytes, or 392 with the UTF-8 name):
//
//	  0  uint32     sensor ID
//	  4  uint32     sensor instance
//	  8  [128]byte  original name (ANSI)
//	136  [128]byte  name shown to the user (ANSI)
//	264  [128]byte  name shown to the user (UTF-8, larger layout only)
//
// Reading element (316 bytes, or 460 with the UTF-8 fields):
//
//	  0  uint32     reading type
//	  4  uint32     index of the sensor the reading belongs to
//	  8  uint32     reading ID
//	 12  [128]byte  original label (ANSI)
//	140  [128]byte  label shown to the user (ANSI)
//	268  [16]byte   unit (ANSI)
//	284  float64    current value
//	292  float64    minimum
//	300  float64    maximum
//	308  float64    average
//	316  [128]byte  label shown to the user (UTF-8, larger layout only)
//	444  [16]byte   unit (UTF-8, larger layout only)
//
// The element sizes are taken from the header rather than assumed, so a
// future HWiNFO that appends fields keeps working.
const (
	sigActive  = "HWiS" // read from HWiNFO 8.60 while its sensors were running
	sigDead    = "DAED" // the 'DEAD' constant as it sits in little-endian memory
	sigDeadAlt = "DEAD" // tolerated in case another build writes it the other way round

	headerSizeV1 = 44
	headerSizeV2 = 48

	nameLen = 128
	unitLen = 16

	sensorSizeV1 = 8 + 2*nameLen          // 264
	sensorSizeV2 = sensorSizeV1 + nameLen // 392

	readingSizeV1 = 12 + 2*nameLen + unitLen + 4*8    // 316
	readingSizeV2 = readingSizeV1 + nameLen + unitLen // 460
)

// header is the decoded fixed part of the block.
type header struct {
	active        bool
	size          int // 44 or 48, depending on the version
	version       uint32
	revision      uint32
	pollTime      int64
	sensorOffset  uint32
	sensorSize    uint32
	sensorCount   uint32
	readingOffset uint32
	readingSize   uint32
	readingCount  uint32
	pollingPeriod uint32 // milliseconds; zero in version 1
}

// parseHeader decodes the header and checks the signature. It does not check
// that the sections fit; see [header.validate].
func parseHeader(data []byte) (header, error) {
	if len(data) < headerSizeV1 {
		return header{}, fmt.Errorf("%w: %d bytes is too short for a header", ErrLayout, len(data))
	}
	var h header
	switch sig := string(data[:4]); sig {
	case sigActive:
		h.active = true
	case sigDead, sigDeadAlt:
	default:
		return header{}, fmt.Errorf("%w: signature %q", ErrSignature, sig)
	}
	le := binary.LittleEndian
	h.version = le.Uint32(data[4:])
	h.revision = le.Uint32(data[8:])
	h.pollTime = int64(le.Uint64(data[12:]))
	h.sensorOffset = le.Uint32(data[20:])
	h.sensorSize = le.Uint32(data[24:])
	h.sensorCount = le.Uint32(data[28:])
	h.readingOffset = le.Uint32(data[32:])
	h.readingSize = le.Uint32(data[36:])
	h.readingCount = le.Uint32(data[40:])
	h.size = headerSizeV1
	if h.version >= 2 {
		if len(data) < headerSizeV2 {
			return header{}, fmt.Errorf("%w: %d bytes is too short for a version %d header", ErrLayout, len(data), h.version)
		}
		h.size = headerSizeV2
		h.pollingPeriod = le.Uint32(data[44:])
	}
	return h, nil
}

// extent returns the number of bytes the header and both sections occupy.
// The arithmetic is done in 64 bits, so hostile counts cannot wrap around.
func (h header) extent() uint64 {
	end := uint64(h.size)
	if h.sensorCount > 0 {
		end = max(end, uint64(h.sensorOffset)+uint64(h.sensorSize)*uint64(h.sensorCount))
	}
	if h.readingCount > 0 {
		end = max(end, uint64(h.readingOffset)+uint64(h.readingSize)*uint64(h.readingCount))
	}
	return end
}

// validate checks that every element the header announces lies inside a block
// of avail bytes and is large enough to hold the fields this package reads.
func (h header) validate(avail uint64) error {
	check := func(kind string, offset, size, count, minSize uint32) error {
		if count == 0 {
			return nil
		}
		if size < minSize {
			return fmt.Errorf("%w: %s elements are %d bytes, expected at least %d", ErrLayout, kind, size, minSize)
		}
		if offset < uint32(h.size) {
			return fmt.Errorf("%w: %s section at offset %d overlaps the header", ErrLayout, kind, offset)
		}
		if end := uint64(offset) + uint64(size)*uint64(count); end > avail {
			return fmt.Errorf("%w: %d %s elements end at byte %d of a %d-byte block", ErrLayout, count, kind, end, avail)
		}
		return nil
	}
	if err := check("sensor", h.sensorOffset, h.sensorSize, h.sensorCount, sensorSizeV1); err != nil {
		return err
	}
	return check("reading", h.readingOffset, h.readingSize, h.readingCount, readingSizeV1)
}

// parseSensor decodes one sensor element. b must hold at least sensorSizeV1
// bytes; the UTF-8 name is used when the element is large enough to carry it.
func parseSensor(b []byte, index int) Sensor {
	le := binary.LittleEndian
	s := Sensor{
		Index:        index,
		ID:           le.Uint32(b[0:]),
		Instance:     le.Uint32(b[4:]),
		OriginalName: decodeANSI(b[8 : 8+nameLen]),
	}
	var utf []byte
	if len(b) >= sensorSizeV2 {
		utf = b[sensorSizeV1:sensorSizeV2]
	}
	s.Name = userString(utf, b[8+nameLen:sensorSizeV1], s.OriginalName)
	return s
}

// parseReading decodes one reading element. b must hold at least
// readingSizeV1 bytes. A sensor index that does not exist becomes -1.
func parseReading(b []byte, index, sensors int) Reading {
	le := binary.LittleEndian
	r := Reading{
		Index:         index,
		Type:          ReadingType(le.Uint32(b[0:])),
		SensorIndex:   -1,
		ID:            le.Uint32(b[8:]),
		OriginalLabel: decodeANSI(b[12 : 12+nameLen]),
		Value:         math.Float64frombits(le.Uint64(b[284:])),
		Min:           math.Float64frombits(le.Uint64(b[292:])),
		Max:           math.Float64frombits(le.Uint64(b[300:])),
		Avg:           math.Float64frombits(le.Uint64(b[308:])),
	}
	if si := le.Uint32(b[4:]); uint64(si) < uint64(sensors) {
		r.SensorIndex = int(si)
	}
	var utfLabel, utfUnit []byte
	if len(b) >= readingSizeV2 {
		utfLabel = b[readingSizeV1 : readingSizeV1+nameLen]
		utfUnit = b[readingSizeV1+nameLen : readingSizeV2]
	}
	r.Label = userString(utfLabel, b[12+nameLen:12+2*nameLen], r.OriginalLabel)
	r.Unit = userString(utfUnit, b[268:268+unitLen], "")
	return r
}

// cString returns the bytes of a NUL-terminated field without the terminator.
func cString(b []byte) []byte {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		return b[:i]
	}
	return b
}

// userString picks the best available text for a user-facing field: the
// UTF-8 copy when the layout has one and it is well formed, otherwise the
// legacy ANSI copy, otherwise fallback.
func userString(utf, ansi []byte, fallback string) string {
	if u := cString(utf); len(u) > 0 && utf8.Valid(u) {
		return string(u)
	}
	if a := cString(ansi); len(a) > 0 {
		return decodeANSI(a)
	}
	return fallback
}

// cp1252 maps the bytes 0x80-0x9F of Windows-1252 to Unicode. The bytes that
// the code page leaves undefined map to the matching C1 control code.
var cp1252 = [32]rune{
	0x20AC, 0x0081, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021,
	0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0x008D, 0x017D, 0x008F,
	0x0090, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014,
	0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0x009D, 0x017E, 0x0178,
}

// decodeANSI converts a NUL-terminated legacy string to UTF-8.
//
// HWiNFO writes these fields in the system ANSI code page. They are decoded as
// Windows-1252, which is exact for HWiNFO's own English names and for the
// degree sign in units ("°C" is the bytes B0 43). On systems with another
// ANSI code page a renamed sensor may decode wrongly here, but layouts that
// carry UTF-8 copies of the user-facing fields never reach this path for them.
func decodeANSI(b []byte) string {
	b = cString(b)
	ascii := true
	for _, c := range b {
		if c >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return string(b)
	}
	out := make([]rune, len(b))
	for i, c := range b {
		switch {
		case c >= 0x80 && c <= 0x9F:
			out[i] = cp1252[c-0x80]
		default:
			out[i] = rune(c)
		}
	}
	return string(out)
}
