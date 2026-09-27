package rm

import "fmt"

type CrdtID struct {
	Part1 uint8
	Part2 uint64
}

type LWW[T any] struct {
	Timestamp CrdtID
	Value     T
}

type RGBA struct {
	A uint8
	B uint8
	G uint8
	R uint8
}

func (c RGBA) Hex() string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

type Point struct {
	Direction float64
	Pressure  float64
	Speed     float64
	Width     float64
	X         float64
	Y         float64
}

type Line struct {
	Color          Color
	ID             CrdtID
	MoveID         *CrdtID
	Points         []Point
	RGBA           *RGBA
	StartingLength float64
	ThicknessScale float64
	Tool           Tool
}

type Rectangle struct {
	H float64
	W float64
	X float64
	Y float64
}

type GlyphRange struct {
	Color      Color
	ID         CrdtID
	Length     uint32
	RGBA       *RGBA
	Rectangles []Rectangle
	Start      *uint32
	Text       string
}

type Group struct {
	AnchorID        *LWW[CrdtID]
	AnchorOriginX   *LWW[float32]
	AnchorThreshold *LWW[float32]
	AnchorType      *LWW[uint8]
	Children        *Sequence
	Label           LWW[string]
	NodeID          CrdtID
	Visible         LWW[bool]
}

type SequenceItem struct {
	DeletedLength uint32
	ItemID        CrdtID
	LeftID        CrdtID
	RightID       CrdtID
	Value         any
}

type SceneInfo struct {
	BackgroundVisible   *LWW[bool]
	CurrentLayer        LWW[CrdtID]
	PaperSize           *[2]uint32
	RootDocumentVisible *LWW[bool]
}

type Layer struct {
	Highlights []*GlyphRange
	Lines      []*Line
	Name       string
	Visible    bool
}

type Scene struct {
	Info     *SceneInfo
	Root     *Group
	Warnings []string
	nodes    map[CrdtID]*Group
}

var rootID = CrdtID{Part1: 0, Part2: 1}
