package rm

import (
	"fmt"
	"io"
	"math"

	"github.com/cockroachdb/errors"
)

const (
	blockMigrationInfo  = 0x00
	blockSceneTree      = 0x01
	blockTreeNode       = 0x02
	blockSceneGlyphItem = 0x03
	blockSceneGroupItem = 0x04
	blockSceneLineItem  = 0x05
	blockSceneTextItem  = 0x06
	blockRootText       = 0x07
	blockSceneTombstone = 0x08
	blockAuthorIDs      = 0x09
	blockPageInfo       = 0x0A
	blockSceneInfo      = 0x0D
)

const (
	itemTypeGlyph = 0x01
	itemTypeGroup = 0x02
	itemTypeLine  = 0x03
)

type sceneItemBlock struct {
	Item     SequenceItem
	ParentID CrdtID
}

func Parse(data []byte) (*Scene, error) {
	r := newReader(data)
	if err := r.readHeader(); err != nil {
		return nil, err
	}
	scene := &Scene{
		Root:  &Group{NodeID: rootID, Children: newSequence(), Visible: LWW[bool]{Value: true}},
		nodes: map[CrdtID]*Group{},
	}
	scene.nodes[rootID] = scene.Root
	var pending []func() error
	for {
		hdr, err := r.block()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, errors.Wrap(err, "read block header")
		}
		if err := scene.readBlock(r, hdr, &pending); err != nil {
			scene.warn(fmt.Sprintf("block type 0x%02X at %d: %v", hdr.Type, hdr.Start, err))
		}
		if r.pos > hdr.End {
			return nil, errors.Newf("block type 0x%02X at %d overflowed by %d bytes", hdr.Type, hdr.Start, r.pos-hdr.End)
		}
		r.pos = hdr.End
	}
	for _, attach := range pending {
		if err := attach(); err != nil {
			scene.warn(err.Error())
		}
	}
	return scene, nil
}

func (s *Scene) warn(msg string) { s.Warnings = append(s.Warnings, msg) }

func (s *Scene) readBlock(r *reader, hdr *blockHeader, pending *[]func() error) error {
	switch hdr.Type {
	case blockSceneTree:
		treeID, err := r.taggedID(1)
		if err != nil {
			return err
		}
		if _, err := r.taggedID(2); err != nil {
			return err
		}
		if _, err := r.taggedBool(3); err != nil {
			return err
		}
		end, err := r.subblock(4)
		if err != nil {
			return err
		}
		if _, err := r.taggedID(1); err != nil {
			return err
		}
		r.finish(end)
		if _, exists := s.nodes[treeID]; !exists {
			s.nodes[treeID] = &Group{NodeID: treeID, Children: newSequence(), Visible: LWW[bool]{Value: true}}
		}
		return nil
	case blockTreeNode:
		group, err := readTreeNode(r, hdr)
		if err != nil {
			return err
		}
		node, ok := s.nodes[group.NodeID]
		if !ok {
			node = &Group{NodeID: group.NodeID, Children: newSequence()}
			s.nodes[group.NodeID] = node
		}
		node.AnchorID = group.AnchorID
		node.AnchorOriginX = group.AnchorOriginX
		node.AnchorThreshold = group.AnchorThreshold
		node.AnchorType = group.AnchorType
		node.Label = group.Label
		node.Visible = group.Visible
		return nil
	case blockSceneGlyphItem, blockSceneGroupItem, blockSceneLineItem, blockSceneTextItem, blockSceneTombstone:
		item, err := readSceneItem(r, hdr)
		if err != nil {
			return err
		}
		if item.Item.Value == nil {
			return nil
		}
		*pending = append(*pending, func() error { return s.attach(item) })
		return nil
	case blockSceneInfo:
		info, err := readSceneInfo(r, hdr)
		if err != nil {
			return err
		}
		s.Info = info
		return nil
	default:
		return nil
	}
}

func (s *Scene) attach(item *sceneItemBlock) error {
	parent, ok := s.nodes[item.ParentID]
	if !ok {
		return errors.Newf("item %s references unknown parent %s", item.Item.ItemID, item.ParentID)
	}
	if childID, ok := item.Item.Value.(CrdtID); ok {
		node, ok := s.nodes[childID]
		if !ok {
			return errors.Newf("group item %s references unknown node %s", item.Item.ItemID, childID)
		}
		item.Item.Value = node
	}
	parent.Children.add(item.Item)
	return nil
}

func readTreeNode(r *reader, hdr *blockHeader) (*Group, error) {
	nodeID, err := r.taggedID(1)
	if err != nil {
		return nil, err
	}
	label, err := r.lwwString(2)
	if err != nil {
		return nil, err
	}
	visible, err := r.lwwBool(3)
	if err != nil {
		return nil, err
	}
	group := &Group{Label: label, NodeID: nodeID, Visible: visible}
	if r.pos < hdr.End && r.checkTag(7, tagLength4) {
		anchorID, err := r.lwwID(7)
		if err != nil {
			return nil, err
		}
		anchorType, err := r.lwwByte(8)
		if err != nil {
			return nil, err
		}
		threshold, err := r.lwwFloat32(9)
		if err != nil {
			return nil, err
		}
		originX, err := r.lwwFloat32(10)
		if err != nil {
			return nil, err
		}
		group.AnchorID = &anchorID
		group.AnchorOriginX = &originX
		group.AnchorThreshold = &threshold
		group.AnchorType = &anchorType
	}
	return group, nil
}

func readSceneItem(r *reader, hdr *blockHeader) (*sceneItemBlock, error) {
	parentID, err := r.taggedID(1)
	if err != nil {
		return nil, err
	}
	itemID, err := r.taggedID(2)
	if err != nil {
		return nil, err
	}
	leftID, err := r.taggedID(3)
	if err != nil {
		return nil, err
	}
	rightID, err := r.taggedID(4)
	if err != nil {
		return nil, err
	}
	deletedLength, err := r.taggedUint32(5)
	if err != nil {
		return nil, err
	}
	block := &sceneItemBlock{
		Item:     SequenceItem{DeletedLength: deletedLength, ItemID: itemID, LeftID: leftID, RightID: rightID},
		ParentID: parentID,
	}
	if !r.hasSubblock(6, hdr.End) {
		return block, nil
	}
	end, err := r.subblock(6)
	if err != nil {
		return nil, err
	}
	itemType, err := r.uint8()
	if err != nil {
		return nil, err
	}
	switch {
	case hdr.Type == blockSceneLineItem && itemType == itemTypeLine:
		line, err := readLine(r, hdr.CurrentVersion, end)
		if err != nil {
			return nil, err
		}
		line.ID = itemID
		block.Item.Value = line
	case hdr.Type == blockSceneGlyphItem && itemType == itemTypeGlyph:
		glyph, err := readGlyphRange(r, end)
		if err != nil {
			return nil, err
		}
		glyph.ID = itemID
		block.Item.Value = glyph
	case hdr.Type == blockSceneGroupItem && itemType == itemTypeGroup:
		nodeID, err := r.taggedID(2)
		if err != nil {
			return nil, err
		}
		block.Item.Value = nodeID
	}
	r.finish(end)
	return block, nil
}

func readLine(r *reader, version uint8, end int) (*Line, error) {
	tool, err := r.taggedUint32(1)
	if err != nil {
		return nil, err
	}
	color, err := r.taggedUint32(2)
	if err != nil {
		return nil, err
	}
	thickness, err := r.taggedFloat64(3)
	if err != nil {
		return nil, err
	}
	startingLength, err := r.taggedFloat32(4)
	if err != nil {
		return nil, err
	}
	pointsEnd, err := r.subblock(5)
	if err != nil {
		return nil, err
	}
	pointSize := 14
	if version == 1 {
		pointSize = 24
	}
	dataLength := pointsEnd - r.pos
	if dataLength%pointSize != 0 {
		return nil, errors.Newf("point data length %d is not a multiple of %d", dataLength, pointSize)
	}
	points := make([]Point, 0, dataLength/pointSize)
	for range dataLength / pointSize {
		p, err := readPoint(r, version)
		if err != nil {
			return nil, err
		}
		points = append(points, p)
	}
	r.finish(pointsEnd)
	line := &Line{
		Color:          Color(color),
		Points:         points,
		StartingLength: float64(startingLength),
		ThicknessScale: thickness,
		Tool:           Tool(tool),
	}
	if _, err := r.taggedID(6); err != nil {
		return nil, err
	}
	if end-r.pos >= 3 && r.checkTag(7, tagID) {
		moveID, err := r.taggedID(7)
		if err != nil {
			return nil, err
		}
		line.MoveID = &moveID
	}
	if r.pos < end {
		rgba, err := r.optionalColor(8)
		if err != nil {
			return nil, err
		}
		line.RGBA = rgba
	}
	return line, nil
}

func readPoint(r *reader, version uint8) (Point, error) {
	x, err := r.float32()
	if err != nil {
		return Point{}, err
	}
	y, err := r.float32()
	if err != nil {
		return Point{}, err
	}
	p := Point{X: float64(x), Y: float64(y)}
	if version == 1 {
		speed, err := r.float32()
		if err != nil {
			return Point{}, err
		}
		direction, err := r.float32()
		if err != nil {
			return Point{}, err
		}
		width, err := r.float32()
		if err != nil {
			return Point{}, err
		}
		pressure, err := r.float32()
		if err != nil {
			return Point{}, err
		}
		p.Direction = 255 * float64(direction) / (math.Pi * 2)
		p.Pressure = float64(pressure) * 255
		p.Speed = float64(speed) * 4
		p.Width = math.Round(float64(width) * 4)
		return p, nil
	}
	speed, err := r.uint16()
	if err != nil {
		return Point{}, err
	}
	width, err := r.uint16()
	if err != nil {
		return Point{}, err
	}
	direction, err := r.uint8()
	if err != nil {
		return Point{}, err
	}
	pressure, err := r.uint8()
	if err != nil {
		return Point{}, err
	}
	p.Direction = float64(direction)
	p.Pressure = float64(pressure)
	p.Speed = float64(speed)
	p.Width = float64(width)
	return p, nil
}

func readGlyphRange(r *reader, end int) (*GlyphRange, error) {
	glyph := &GlyphRange{}
	if r.checkTag(2, tagByte4) {
		start, err := r.taggedUint32(2)
		if err != nil {
			return nil, err
		}
		glyph.Start = &start
	}
	hasLength := false
	if r.checkTag(3, tagByte4) {
		length, err := r.taggedUint32(3)
		if err != nil {
			return nil, err
		}
		glyph.Length = length
		hasLength = true
	}
	color, err := r.taggedUint32(4)
	if err != nil {
		return nil, err
	}
	glyph.Color = Color(color)
	text, err := r.string(5)
	if err != nil {
		return nil, err
	}
	glyph.Text = text
	if !hasLength {
		glyph.Length = uint32(len([]rune(text)))
	}
	rectsEnd, err := r.subblock(6)
	if err != nil {
		return nil, err
	}
	count, err := r.varuint()
	if err != nil {
		return nil, err
	}
	if count > uint64(rectsEnd-r.pos)/32 {
		return nil, errors.Newf("glyph rectangle count %d exceeds subblock", count)
	}
	for range count {
		var rect Rectangle
		for _, dst := range []*float64{&rect.X, &rect.Y, &rect.W, &rect.H} {
			v, err := r.float64()
			if err != nil {
				return nil, err
			}
			*dst = v
		}
		glyph.Rectangles = append(glyph.Rectangles, rect)
	}
	r.finish(rectsEnd)
	if r.pos < end {
		rgba, err := r.optionalColor(10)
		if err != nil {
			return nil, err
		}
		glyph.RGBA = rgba
	}
	return glyph, nil
}

func readSceneInfo(r *reader, hdr *blockHeader) (*SceneInfo, error) {
	current, err := r.lwwID(1)
	if err != nil {
		return nil, err
	}
	info := &SceneInfo{CurrentLayer: current}
	if r.pos < hdr.End && r.checkTag(2, tagLength4) {
		v, err := r.lwwBool(2)
		if err != nil {
			return nil, err
		}
		info.BackgroundVisible = &v
	}
	if r.pos < hdr.End && r.checkTag(3, tagLength4) {
		v, err := r.lwwBool(3)
		if err != nil {
			return nil, err
		}
		info.RootDocumentVisible = &v
	}
	if r.pos < hdr.End && r.checkTag(5, tagLength4) {
		pair, err := r.intPair(5)
		if err != nil {
			return nil, err
		}
		info.PaperSize = &pair
	}
	return info, nil
}
