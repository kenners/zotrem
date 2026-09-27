package render

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"

	"github.com/kenners/zotrem/pkg/rm"
	"github.com/kenners/zotrem/pkg/zotero"
)

const (
	KindHighlight = "highlight"
	KindInk       = "ink"
)

type Annotation struct {
	Color     string
	Key       string
	Kind      string
	PageIndex int
	PageLabel string
	Paths     [][]float64
	Rects     []Rect
	SortIndex string
	Text      string
	Width     float64
}

type PageScene struct {
	Box       PageBox
	PageID    string
	PageIndex int
	Scene     *rm.Scene
}

func Annotations(page PageScene, dpi float64) []Annotation {
	mapper := NewMapper(page.Box, dpi)
	var out []Annotation
	if page.Scene == nil {
		return out
	}
	for _, layer := range page.Scene.Layers() {
		if !layer.Visible {
			continue
		}
		for _, glyph := range layer.Highlights {
			if ann, ok := highlightAnnotation(page, mapper, glyph); ok {
				out = append(out, ann)
			}
		}
		for _, line := range layer.Lines {
			if ann, ok := lineAnnotation(page, mapper, line); ok {
				out = append(out, ann)
			}
		}
	}
	return out
}

func highlightAnnotation(page PageScene, mapper Mapper, glyph *rm.GlyphRange) (Annotation, bool) {
	if len(glyph.Rectangles) == 0 {
		return Annotation{}, false
	}
	rects := make([]Rect, 0, len(glyph.Rectangles))
	top := math.Inf(1)
	for _, r := range glyph.Rectangles {
		rects = append(rects, mapper.UserRect(r.X, r.Y, r.W, r.H))
		_, v := mapper.ToDisplay(r.X, r.Y)
		top = math.Min(top, v)
	}
	sortRects(rects)
	return Annotation{
		Color:     glyph.EffectiveColor().Hex(),
		Key:       fmt.Sprintf("%s/%s", page.PageID, glyph.ID),
		Kind:      KindHighlight,
		PageIndex: page.PageIndex,
		PageLabel: strconv.Itoa(page.PageIndex + 1),
		Rects:     rects,
		SortIndex: sortIndex(page.PageIndex, top),
		Text:      glyph.Text,
	}, true
}

func lineAnnotation(page PageScene, mapper Mapper, line *rm.Line) (Annotation, bool) {
	if line.Tool.IsEraser() || len(line.Points) == 0 {
		return Annotation{}, false
	}
	if line.Tool.IsHighlighter() {
		return highlighterStroke(page, mapper, line)
	}
	path := make([]float64, 0, len(line.Points)*2)
	top := math.Inf(1)
	var lastX, lastY float64
	for i, p := range line.Points {
		x, y := mapper.ToUser(p.X, p.Y)
		x, y = round2(x), round2(y)
		if i > 0 && x == lastX && y == lastY {
			continue
		}
		path = append(path, x, y)
		lastX, lastY = x, y
		_, v := mapper.ToDisplay(p.X, p.Y)
		top = math.Min(top, v)
	}
	if len(path) == 2 {
		path = append(path, path[0]+0.1, path[1])
	}
	return Annotation{
		Color:     line.EffectiveColor().Hex(),
		Key:       fmt.Sprintf("%s/%s", page.PageID, line.ID),
		Kind:      KindInk,
		PageIndex: page.PageIndex,
		PageLabel: strconv.Itoa(page.PageIndex + 1),
		Paths:     [][]float64{path},
		SortIndex: sortIndex(page.PageIndex, top),
		Width:     math.Max(0.5, round2(mapper.Length(StrokeWidth(line)))),
	}, true
}

func highlighterStroke(page PageScene, mapper Mapper, line *rm.Line) (Annotation, bool) {
	half := StrokeWidth(line) / 2
	minX, maxX := math.Inf(1), math.Inf(-1)
	minY, maxY := math.Inf(1), math.Inf(-1)
	for _, p := range line.Points {
		minX, maxX = math.Min(minX, p.X), math.Max(maxX, p.X)
		minY, maxY = math.Min(minY, p.Y), math.Max(maxY, p.Y)
	}
	rect := mapper.UserRect(minX-half, minY-half, maxX-minX+2*half, maxY-minY+2*half)
	_, top := mapper.ToDisplay(minX, minY)
	return Annotation{
		Color:     line.EffectiveColor().Hex(),
		Key:       fmt.Sprintf("%s/%s", page.PageID, line.ID),
		Kind:      KindHighlight,
		PageIndex: page.PageIndex,
		PageLabel: strconv.Itoa(page.PageIndex + 1),
		Rects:     []Rect{rect},
		SortIndex: sortIndex(page.PageIndex, top),
	}, true
}

func StrokeWidth(line *rm.Line) float64 {
	scale := line.ThicknessScale
	if scale <= 0 {
		scale = 2
	}
	switch {
	case line.Tool.IsHighlighter():
		return 15
	case line.Tool.IsShader():
		return 12
	case line.Tool == rm.ToolFineliner1 || line.Tool == rm.ToolFineliner2:
		return scale * 1.8
	case line.Tool == rm.ToolMechanicalPencil1 || line.Tool == rm.ToolMechanicalPencil2:
		return scale * scale
	case line.Tool == rm.ToolPaintbrush1 || line.Tool == rm.ToolPaintbrush2 || line.Tool == rm.ToolCalligraphy:
		return scale * 2.2
	case line.Tool == rm.ToolMarker1 || line.Tool == rm.ToolMarker2:
		return scale * 2.5
	default:
		return scale * 1.4
	}
}

func StrokeOpacity(line *rm.Line) float64 {
	switch {
	case line.Tool.IsHighlighter():
		return 0.35
	case line.Tool.IsShader():
		return 0.15
	case line.Tool == rm.ToolPencil1 || line.Tool == rm.ToolPencil2:
		return 0.85
	default:
		return 1
	}
}

func sortIndex(pageIndex int, top float64) string {
	if math.IsInf(top, 0) || math.IsNaN(top) || top < 0 {
		top = 0
	}
	return fmt.Sprintf("%05d|%06d|%05d", pageIndex, 0, min(int(top), 99999))
}

type inkPosition struct {
	PageIndex int         `json:"pageIndex"`
	Paths     [][]float64 `json:"paths"`
	Width     float64     `json:"width"`
}

type highlightPosition struct {
	PageIndex int          `json:"pageIndex"`
	Rects     [][4]float64 `json:"rects"`
}

func (a Annotation) Position() string {
	var pos any
	switch a.Kind {
	case KindInk:
		pos = inkPosition{PageIndex: a.PageIndex, Paths: a.Paths, Width: a.Width}
	default:
		rects := make([][4]float64, 0, len(a.Rects))
		for _, r := range a.Rects {
			rects = append(rects, [4]float64{round2(r.X1), round2(r.Y1), round2(r.X2), round2(r.Y2)})
		}
		pos = highlightPosition{PageIndex: a.PageIndex, Rects: rects}
	}
	data, _ := json.Marshal(pos)
	return string(data)
}

func (a Annotation) ItemData(parentKey string) zotero.ItemData {
	data := zotero.ItemData{
		AnnotationColor:     a.Color,
		AnnotationPageLabel: a.PageLabel,
		AnnotationPosition:  a.Position(),
		AnnotationSortIndex: a.SortIndex,
		AnnotationType:      a.Kind,
		ItemType:            zotero.ItemTypeAnnotation,
		ParentItem:          parentKey,
		Tags:                []zotero.Tag{},
	}
	if a.Kind == KindHighlight {
		data.AnnotationText = a.Text
	}
	return data
}

func (a Annotation) Signature() string {
	return a.Kind + "|" + a.Color + "|" + a.Position()
}
