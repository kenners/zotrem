package render

import (
	"math"
	"sort"
)

const SceneUnitsPerInch = 226.0

type PageBox struct {
	LLX    float64
	LLY    float64
	Rotate int
	URX    float64
	URY    float64
}

func (b PageBox) Width() float64  { return b.URX - b.LLX }
func (b PageBox) Height() float64 { return b.URY - b.LLY }

func (b PageBox) rotation() int {
	r := b.Rotate % 360
	if r < 0 {
		r += 360
	}
	return r
}

func (b PageBox) DisplayWidth() float64 {
	if b.rotation()%180 != 0 {
		return b.Height()
	}
	return b.Width()
}

func (b PageBox) DisplayHeight() float64 {
	if b.rotation()%180 != 0 {
		return b.Width()
	}
	return b.Height()
}

type Mapper struct {
	Box   PageBox
	Scale float64
}

func NewMapper(box PageBox) Mapper {
	return Mapper{Box: box, Scale: 72 / SceneUnitsPerInch}
}

func (m Mapper) ToDisplay(x, y float64) (float64, float64) {
	return m.Box.DisplayWidth()/2 + x*m.Scale, y * m.Scale
}

func (m Mapper) DisplayToUser(u, v float64) (float64, float64) {
	b := m.Box
	switch b.rotation() {
	case 90:
		return b.LLX + v, b.LLY + u
	case 180:
		return b.URX - u, b.LLY + v
	case 270:
		return b.URX - v, b.URY - u
	default:
		return b.LLX + u, b.URY - v
	}
}

func (m Mapper) ToUser(x, y float64) (float64, float64) {
	u, v := m.ToDisplay(x, y)
	return m.DisplayToUser(u, v)
}

func (m Mapper) Length(units float64) float64 { return units * m.Scale }

type Rect struct {
	X1 float64
	X2 float64
	Y1 float64
	Y2 float64
}

func (m Mapper) UserRect(x, y, w, h float64) Rect {
	corners := [][2]float64{{x, y}, {x + w, y}, {x, y + h}, {x + w, y + h}}
	rect := Rect{X1: math.Inf(1), X2: math.Inf(-1), Y1: math.Inf(1), Y2: math.Inf(-1)}
	for _, c := range corners {
		px, py := m.ToUser(c[0], c[1])
		rect.X1 = math.Min(rect.X1, px)
		rect.X2 = math.Max(rect.X2, px)
		rect.Y1 = math.Min(rect.Y1, py)
		rect.Y2 = math.Max(rect.Y2, py)
	}
	return rect
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func sortRects(rects []Rect) {
	sort.Slice(rects, func(i, j int) bool {
		if rects[i].Y2 != rects[j].Y2 {
			return rects[i].Y2 > rects[j].Y2
		}
		return rects[i].X1 < rects[j].X1
	})
}
