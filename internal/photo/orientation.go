package photo

import "encoding/binary"

const (
	tagOrientation = 0x0112
	typeShort      = 3
	ifdEntrySize   = 12
)

var exifHeader = []byte("Exif\x00\x00")

// orientation returns the EXIF orientation tag (1-8) of a JPEG, or 1 when the
// tag is absent or anything about the data is unexpected. It parses untrusted
// bytes, so every offset and length is checked against the slice before it is
// used, and no input can make it panic.
func orientation(data []byte) int {
	if len(data) < 2 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	pos := 2
	for pos < len(data) {
		if data[pos] != 0xFF {
			return 1
		}
		// A marker may be preceded by any number of 0xFF fill bytes.
		for pos < len(data) && data[pos] == 0xFF {
			pos++
		}
		if pos >= len(data) {
			return 1
		}
		marker := data[pos]
		pos++
		if marker == 0xDA { // start of scan: no more metadata segments
			return 1
		}
		if pos+2 > len(data) {
			return 1
		}
		// The length includes its own two bytes.
		segLen := int(binary.BigEndian.Uint16(data[pos:]))
		if segLen < 2 || pos+segLen > len(data) {
			return 1
		}
		payload := data[pos+2 : pos+segLen]
		if marker == 0xE1 && len(payload) >= len(exifHeader) &&
			string(payload[:len(exifHeader)]) == string(exifHeader) {
			return tiffOrientation(payload[len(exifHeader):])
		}
		pos += segLen
	}
	return 1
}

// tiffOrientation reads the orientation from IFD0 of a TIFF block, whose
// offsets are counted from the block's first byte.
func tiffOrientation(tiff []byte) int {
	if len(tiff) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(tiff[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	if order.Uint16(tiff[2:]) != 42 {
		return 1
	}
	// Offsets are uint32; comparing in uint64 keeps a huge value from
	// overflowing int on a 32-bit platform.
	ifd := uint64(order.Uint32(tiff[4:]))
	if ifd+2 > uint64(len(tiff)) {
		return 1
	}
	count := uint64(order.Uint16(tiff[ifd:]))
	entries := ifd + 2
	if entries+count*ifdEntrySize > uint64(len(tiff)) {
		return 1
	}
	for i := range count {
		e := tiff[entries+i*ifdEntrySize : entries+(i+1)*ifdEntrySize]
		if order.Uint16(e) != tagOrientation {
			continue
		}
		if order.Uint16(e[2:]) != typeShort {
			return 1
		}
		if v := int(order.Uint16(e[8:])); v >= 1 && v <= 8 {
			return v
		}
		return 1
	}
	return 1
}
