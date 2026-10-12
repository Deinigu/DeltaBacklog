package shots

import (
	"bytes"
	"encoding/binary"
	"io"
)

// readEXIF returns the orientation (1 when absent) and DateTimeOriginal ("2006-01-02T15:04:05") of a JPEG. It reads
// only the APP1 segment, never the pixels, and returns the defaults on anything unexpected.
func readEXIF(r io.Reader) (orientation int, taken string) {
	orientation = 1
	var soi [2]byte
	if _, err := io.ReadFull(r, soi[:]); err != nil || soi != [2]byte{0xFF, 0xD8} {
		return
	}
	for i := 0; i < 20; i++ { // the EXIF segment is among the first few
		var m [4]byte
		if _, err := io.ReadFull(r, m[:]); err != nil || m[0] != 0xFF {
			return
		}
		n := int(binary.BigEndian.Uint16(m[2:])) - 2
		if n < 0 || m[1] == 0xDA { // start of scan: no EXIF after it
			return
		}
		seg := make([]byte, n)
		if _, err := io.ReadFull(r, seg); err != nil {
			return
		}
		if m[1] == 0xE1 && bytes.HasPrefix(seg, []byte("Exif\x00\x00")) {
			return parseTIFF(seg[6:])
		}
	}
	return
}

func parseTIFF(t []byte) (orientation int, taken string) {
	orientation = 1
	if len(t) < 8 {
		return
	}
	var bo binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return
	}
	// entries calls fn for each 12-byte entry of the IFD at off.
	entries := func(off uint32, fn func(tag, typ uint16, count, val uint32)) {
		if int64(off)+2 > int64(len(t)) {
			return
		}
		n := int(bo.Uint16(t[off:]))
		for i := 0; i < n; i++ {
			p := int64(off) + 2 + int64(i)*12
			if p+12 > int64(len(t)) {
				return
			}
			e := t[p : p+12]
			fn(bo.Uint16(e), bo.Uint16(e[2:]), bo.Uint32(e[4:]), bo.Uint32(e[8:]))
		}
	}
	var exifOff uint32
	entries(bo.Uint32(t[4:]), func(tag, typ uint16, count, val uint32) {
		switch tag {
		case 0x0112: // orientation: a SHORT, stored first in the 4-byte value field
			v := val & 0xFFFF
			if bo == binary.BigEndian {
				v = val >> 16
			}
			if v >= 1 && v <= 8 {
				orientation = int(v)
			}
		case 0x8769:
			exifOff = val
		}
	})
	if exifOff != 0 {
		entries(exifOff, func(tag, typ uint16, count, val uint32) {
			if tag == 0x9003 && typ == 2 && count == 20 && int64(val)+19 <= int64(len(t)) { // "YYYY:MM:DD HH:MM:SS\0"
				s := string(t[val : val+19])
				if s[4] == ':' && s[7] == ':' && s[10] == ' ' && s[13] == ':' && s[16] == ':' && s[:4] != "0000" {
					taken = s[:4] + "-" + s[5:7] + "-" + s[8:10] + "T" + s[11:]
				}
			}
		})
	}
	return
}
