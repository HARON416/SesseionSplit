package main

// A small fragmented-MP4 writer for scrcpy's H.264 and AAC packets. Encoded
// samples retain the device's timestamps; there is no decode/re-encode step.
import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

func mp4u32(values ...uint32) []byte {
	b := make([]byte, 4*len(values))
	for i, v := range values {
		binary.BigEndian.PutUint32(b[i*4:], v)
	}
	return b
}
func mp4u16(values ...uint16) []byte {
	b := make([]byte, 2*len(values))
	for i, v := range values {
		binary.BigEndian.PutUint16(b[i*2:], v)
	}
	return b
}
func mp4box(kind string, parts ...[]byte) []byte {
	b := make([]byte, 8)
	copy(b[4:], kind)
	for _, p := range parts {
		b = append(b, p...)
	}
	binary.BigEndian.PutUint32(b, uint32(len(b)))
	return b
}
func mp4full(kind string, flags uint32, parts ...[]byte) []byte {
	return mp4box(kind, append([][]byte{mp4u32(flags)}, parts...)...)
}

var mp4matrix = mp4u32(0x10000, 0, 0, 0, 0x10000, 0, 0, 0, 0x40000000)

func annexBNALs(data []byte) [][]byte {
	var units [][]byte
	start := -1
	for i := 0; i+3 <= len(data); {
		size := 0
		if i+4 <= len(data) && bytes.Equal(data[i:i+4], []byte{0, 0, 0, 1}) {
			size = 4
		} else if bytes.Equal(data[i:i+3], []byte{0, 0, 1}) {
			size = 3
		}
		if size > 0 {
			if start >= 0 && i > start {
				units = append(units, data[start:i])
			}
			start = i + size
			i += size
		} else {
			i++
		}
	}
	if start >= 0 && start < len(data) {
		units = append(units, data[start:])
	}
	return units
}
func avcSample(data []byte) ([]byte, error) {
	units := annexBNALs(data)
	if len(units) == 0 {
		return nil, errors.New("Invalid H.264 packet")
	}
	var result []byte
	for _, unit := range units {
		result = append(result, mp4u32(uint32(len(unit)))...)
		result = append(result, unit...)
	}
	return result, nil
}
func avcConfiguration(data []byte) ([]byte, string, error) {
	var sps, pps []byte
	for _, unit := range annexBNALs(data) {
		switch unit[0] & 31 {
		case 7:
			sps = unit
		case 8:
			pps = unit
		}
	}
	if len(sps) < 4 || len(pps) == 0 || len(sps) > 65535 || len(pps) > 65535 {
		return nil, "", errors.New("H.264 configuration is missing SPS/PPS")
	}
	result := []byte{1, sps[1], sps[2], sps[3], 0xff, 0xe1}
	result = append(result, mp4u16(uint16(len(sps)))...)
	result = append(result, sps...)
	result = append(result, 1)
	result = append(result, mp4u16(uint16(len(pps)))...)
	result = append(result, pps...)
	return result, fmt.Sprintf("avc1.%02X%02X%02X", sps[1], sps[2], sps[3]), nil
}
func mp4descriptor(tag byte, data []byte) []byte { // Configuration descriptors are small, but encode the general ISO length.
	n := len(data)
	size := []byte{byte(n & 127)}
	for n >>= 7; n > 0; n >>= 7 {
		size = append([]byte{byte(n&127) | 128}, size...)
	}
	return append(append([]byte{tag}, size...), data...)
}
func mediaInit(video bool, config []byte, width, height int) ([]byte, string, error) {
	var entry []byte
	codec := "mp4a.40.2"
	handler := "soun"
	volume := uint16(0x100)
	mediaHeader := mp4full("smhd", 0, mp4u16(0, 0))
	if video {
		avcc, name, err := avcConfiguration(config)
		if err != nil {
			return nil, "", err
		}
		codec = name
		handler = "vide"
		volume = 0
		mediaHeader = mp4full("vmhd", 1, mp4u16(0, 0, 0, 0))
		entry = mp4box("avc1", make([]byte, 6), mp4u16(1), make([]byte, 16), mp4u16(uint16(width), uint16(height)), mp4u32(0x480000, 0x480000, 0), mp4u16(1), make([]byte, 32), mp4u16(24, 0xffff), mp4box("avcC", avcc))
	} else {
		if len(config) < 2 || config[0]>>3 != 2 {
			return nil, "", errors.New("Expected AAC-LC audio configuration")
		}
		rates := []int{96000, 88200, 64000, 48000, 44100, 32000, 24000, 22050, 16000, 12000, 11025, 8000, 7350}
		rateIndex := int(config[0]&7)<<1 | int(config[1]>>7)
		channels := uint16(config[1] >> 3 & 15)
		if rateIndex >= len(rates) || channels == 0 {
			return nil, "", errors.New("Unsupported AAC configuration")
		}
		decoder := append([]byte{0x40, 0x15, 0, 0, 0}, mp4u32(192000, 192000)...)
		decoder = append(decoder, mp4descriptor(5, config)...)
		es := append([]byte{0, 1, 0}, mp4descriptor(4, decoder)...)
		es = append(es, mp4descriptor(6, []byte{2})...)
		entry = mp4box("mp4a", make([]byte, 6), mp4u16(1), make([]byte, 8), mp4u16(channels, 16, 0, 0), mp4u32(uint32(rates[rateIndex])<<16), mp4full("esds", 0, mp4descriptor(3, es)))
	}
	stbl := mp4box("stbl", mp4full("stsd", 0, mp4u32(1), entry), mp4full("stts", 0, mp4u32(0)), mp4full("stsc", 0, mp4u32(0)), mp4full("stsz", 0, mp4u32(0, 0)), mp4full("stco", 0, mp4u32(0)))
	dinf := mp4box("dinf", mp4full("dref", 0, mp4u32(1), mp4full("url ", 1)))
	mdia := mp4box("mdia", mp4full("mdhd", 0, mp4u32(0, 0, 1000000, 0), mp4u16(0x55c4, 0)), mp4full("hdlr", 0, mp4u32(0), []byte(handler), make([]byte, 12), []byte("Isolate\x00")), mp4box("minf", mediaHeader, dinf, stbl))
	tkhd := mp4full("tkhd", 7, mp4u32(0, 0, 1, 0, 0), make([]byte, 8), mp4u16(0, 0, volume, 0), mp4matrix, mp4u32(uint32(width)<<16, uint32(height)<<16))
	mvhd := mp4full("mvhd", 0, mp4u32(0, 0, 1000000, 0, 0x10000), mp4u16(0x100, 0), make([]byte, 8), mp4matrix, make([]byte, 24), mp4u32(2))
	init := mp4box("ftyp", []byte("iso6"), mp4u32(1), []byte("iso6isommp41"))
	init = append(init, mp4box("moov", mvhd, mp4box("trak", tkhd, mdia), mp4box("mvex", mp4full("trex", 0, mp4u32(1, 1, 0, 0, 0))))...)
	if video {
		return init, "video/mp4; codecs=\"" + codec + "\"", nil
	}
	return init, "audio/mp4; codecs=\"" + codec + "\"", nil
}
func mediaFragment(sequence uint32, pts uint64, duration uint32, key bool, data []byte) []byte {
	flags := uint32(0x1010000)
	if key {
		flags = 0x2000000
	}
	timeBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(timeBytes, pts)
	build := func(offset uint32) []byte {
		return mp4box("moof", mp4full("mfhd", 0, mp4u32(sequence)), mp4box("traf", mp4full("tfhd", 0x20000, mp4u32(1)), mp4full("tfdt", 0x1000000, timeBytes), mp4full("trun", 0x701, mp4u32(1, offset, duration, uint32(len(data)), flags))))
	}
	moof := build(0)
	moof = build(uint32(len(moof) + 8))
	return append(moof, mp4box("mdat", data)...)
}
