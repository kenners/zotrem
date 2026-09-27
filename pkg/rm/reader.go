package rm

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"

	"github.com/cockroachdb/errors"
)

const headerV6 = "reMarkable .lines file, version=6          "

type tagType uint8

const (
	tagByte1   tagType = 0x1
	tagByte4   tagType = 0x4
	tagByte8   tagType = 0x8
	tagLength4 tagType = 0xC
	tagID      tagType = 0xF
)

var errUnexpectedTag = errors.New("unexpected tag")

type reader struct {
	data []byte
	pos  int
}

type blockHeader struct {
	CurrentVersion uint8
	End            int
	MinVersion     uint8
	Start          int
	Type           uint8
}

func newReader(data []byte) *reader {
	return &reader{data: data}
}

func (r *reader) remaining() int { return len(r.data) - r.pos }

func (r *reader) readHeader() error {
	if len(r.data) < len(headerV6) {
		return errors.New("file too short for header")
	}
	if string(r.data[:len(headerV6)]) != headerV6 {
		return errors.Newf("unsupported .rm header %q", string(r.data[:min(len(r.data), len(headerV6))]))
	}
	r.pos = len(headerV6)
	return nil
}

func (r *reader) bytes(n int) ([]byte, error) {
	if n < 0 || r.pos+n > len(r.data) {
		return nil, io.ErrUnexpectedEOF
	}
	out := r.data[r.pos : r.pos+n]
	r.pos += n
	return out, nil
}

func (r *reader) uint8() (uint8, error) {
	b, err := r.bytes(1)
	if err != nil {
		return 0, err
	}
	return b[0], nil
}

func (r *reader) bool() (bool, error) {
	v, err := r.uint8()
	return v != 0, err
}

func (r *reader) uint16() (uint16, error) {
	b, err := r.bytes(2)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint16(b), nil
}

func (r *reader) uint32() (uint32, error) {
	b, err := r.bytes(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(b), nil
}

func (r *reader) float32() (float32, error) {
	v, err := r.uint32()
	return math.Float32frombits(v), err
}

func (r *reader) float64() (float64, error) {
	b, err := r.bytes(8)
	if err != nil {
		return 0, err
	}
	return math.Float64frombits(binary.LittleEndian.Uint64(b)), nil
}

func (r *reader) varuint() (uint64, error) {
	var result uint64
	var shift uint
	for {
		b, err := r.uint8()
		if err != nil {
			return 0, err
		}
		result |= uint64(b&0x7F) << shift
		shift += 7
		if b&0x80 == 0 {
			return result, nil
		}
		if shift > 63 {
			return 0, errors.New("varuint overflow")
		}
	}
}

func (r *reader) crdtID() (CrdtID, error) {
	part1, err := r.uint8()
	if err != nil {
		return CrdtID{}, err
	}
	part2, err := r.varuint()
	if err != nil {
		return CrdtID{}, err
	}
	return CrdtID{Part1: part1, Part2: part2}, nil
}

func (r *reader) peekTag() (int, tagType, error) {
	pos := r.pos
	defer func() { r.pos = pos }()
	x, err := r.varuint()
	if err != nil {
		return 0, 0, err
	}
	return int(x >> 4), tagType(x & 0xF), nil
}

func (r *reader) checkTag(index int, typ tagType) bool {
	gotIndex, gotType, err := r.peekTag()
	return err == nil && gotIndex == index && gotType == typ
}

func (r *reader) tag(index int, typ tagType) error {
	pos := r.pos
	x, err := r.varuint()
	if err != nil {
		return err
	}
	gotIndex, gotType := int(x>>4), tagType(x&0xF)
	if gotIndex != index || gotType != typ {
		r.pos = pos
		return errors.Wrapf(errUnexpectedTag, "expected index %d type 0x%X, got index %d type 0x%X at %d", index, typ, gotIndex, gotType, pos)
	}
	return nil
}

func (r *reader) taggedID(index int) (CrdtID, error) {
	if err := r.tag(index, tagID); err != nil {
		return CrdtID{}, err
	}
	return r.crdtID()
}

func (r *reader) taggedBool(index int) (bool, error) {
	if err := r.tag(index, tagByte1); err != nil {
		return false, err
	}
	return r.bool()
}

func (r *reader) taggedByte(index int) (uint8, error) {
	if err := r.tag(index, tagByte1); err != nil {
		return 0, err
	}
	return r.uint8()
}

func (r *reader) taggedUint32(index int) (uint32, error) {
	if err := r.tag(index, tagByte4); err != nil {
		return 0, err
	}
	return r.uint32()
}

func (r *reader) taggedFloat32(index int) (float32, error) {
	if err := r.tag(index, tagByte4); err != nil {
		return 0, err
	}
	return r.float32()
}

func (r *reader) taggedFloat64(index int) (float64, error) {
	if err := r.tag(index, tagByte8); err != nil {
		return 0, err
	}
	return r.float64()
}

func (r *reader) subblock(index int) (end int, err error) {
	if err := r.tag(index, tagLength4); err != nil {
		return 0, err
	}
	length, err := r.uint32()
	if err != nil {
		return 0, err
	}
	end = r.pos + int(length)
	if end > len(r.data) {
		return 0, errors.Wrapf(io.ErrUnexpectedEOF, "subblock %d of length %d at %d", index, length, r.pos)
	}
	return end, nil
}

func (r *reader) finish(end int) {
	if r.pos < end {
		r.pos = end
	}
}

func (r *reader) hasSubblock(index int, blockEnd int) bool {
	if r.pos >= blockEnd {
		return false
	}
	return r.checkTag(index, tagLength4)
}

func (r *reader) block() (*blockHeader, error) {
	if r.remaining() == 0 {
		return nil, io.EOF
	}
	length, err := r.uint32()
	if err != nil {
		return nil, err
	}
	unknown, err := r.uint8()
	if err != nil {
		return nil, err
	}
	minVersion, err := r.uint8()
	if err != nil {
		return nil, err
	}
	currentVersion, err := r.uint8()
	if err != nil {
		return nil, err
	}
	blockType, err := r.uint8()
	if err != nil {
		return nil, err
	}
	if unknown != 0 {
		return nil, errors.Newf("unexpected block header byte %d at %d", unknown, r.pos-4)
	}
	end := r.pos + int(length)
	if end > len(r.data) {
		return nil, errors.Wrapf(io.ErrUnexpectedEOF, "block type 0x%X of length %d at %d", blockType, length, r.pos)
	}
	return &blockHeader{
		CurrentVersion: currentVersion,
		End:            end,
		MinVersion:     minVersion,
		Start:          r.pos,
		Type:           blockType,
	}, nil
}

func (r *reader) lwwBool(index int) (LWW[bool], error) {
	end, err := r.subblock(index)
	if err != nil {
		return LWW[bool]{}, err
	}
	ts, err := r.taggedID(1)
	if err != nil {
		return LWW[bool]{}, err
	}
	v, err := r.taggedBool(2)
	if err != nil {
		return LWW[bool]{}, err
	}
	r.finish(end)
	return LWW[bool]{Timestamp: ts, Value: v}, nil
}

func (r *reader) lwwByte(index int) (LWW[uint8], error) {
	end, err := r.subblock(index)
	if err != nil {
		return LWW[uint8]{}, err
	}
	ts, err := r.taggedID(1)
	if err != nil {
		return LWW[uint8]{}, err
	}
	v, err := r.taggedByte(2)
	if err != nil {
		return LWW[uint8]{}, err
	}
	r.finish(end)
	return LWW[uint8]{Timestamp: ts, Value: v}, nil
}

func (r *reader) lwwFloat32(index int) (LWW[float32], error) {
	end, err := r.subblock(index)
	if err != nil {
		return LWW[float32]{}, err
	}
	ts, err := r.taggedID(1)
	if err != nil {
		return LWW[float32]{}, err
	}
	v, err := r.taggedFloat32(2)
	if err != nil {
		return LWW[float32]{}, err
	}
	r.finish(end)
	return LWW[float32]{Timestamp: ts, Value: v}, nil
}

func (r *reader) lwwID(index int) (LWW[CrdtID], error) {
	end, err := r.subblock(index)
	if err != nil {
		return LWW[CrdtID]{}, err
	}
	ts, err := r.taggedID(1)
	if err != nil {
		return LWW[CrdtID]{}, err
	}
	v, err := r.taggedID(2)
	if err != nil {
		return LWW[CrdtID]{}, err
	}
	r.finish(end)
	return LWW[CrdtID]{Timestamp: ts, Value: v}, nil
}

func (r *reader) lwwString(index int) (LWW[string], error) {
	end, err := r.subblock(index)
	if err != nil {
		return LWW[string]{}, err
	}
	ts, err := r.taggedID(1)
	if err != nil {
		return LWW[string]{}, err
	}
	v, err := r.string(2)
	if err != nil {
		return LWW[string]{}, err
	}
	r.finish(end)
	return LWW[string]{Timestamp: ts, Value: v}, nil
}

func (r *reader) string(index int) (string, error) {
	end, err := r.subblock(index)
	if err != nil {
		return "", err
	}
	length, err := r.varuint()
	if err != nil {
		return "", err
	}
	if _, err := r.bool(); err != nil {
		return "", err
	}
	if r.pos+int(length) > end {
		return "", errors.Newf("string of length %d overflows subblock ending at %d", length, end)
	}
	b, err := r.bytes(int(length))
	if err != nil {
		return "", err
	}
	r.finish(end)
	return string(b), nil
}

func (r *reader) intPair(index int) ([2]uint32, error) {
	end, err := r.subblock(index)
	if err != nil {
		return [2]uint32{}, err
	}
	first, err := r.uint32()
	if err != nil {
		return [2]uint32{}, err
	}
	second, err := r.uint32()
	if err != nil {
		return [2]uint32{}, err
	}
	r.finish(end)
	return [2]uint32{first, second}, nil
}

func (r *reader) optionalColor(index int) (*RGBA, error) {
	if !r.checkTag(index, tagByte4) {
		return nil, nil
	}
	packed, err := r.taggedUint32(index)
	if err != nil {
		return nil, err
	}
	return &RGBA{
		A: uint8(packed >> 24),
		B: uint8(packed),
		G: uint8(packed >> 8),
		R: uint8(packed >> 16),
	}, nil
}

func (c CrdtID) String() string { return fmt.Sprintf("%d:%d", c.Part1, c.Part2) }
