package render

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"codeberg.org/go-pdf/fpdf"
	"github.com/kenners/zotrem/pkg/rm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func letter(rotate int) PageBox { return PageBox{LLX: 0, LLY: 0, Rotate: rotate, URX: 612, URY: 792} }

func TestMapperRotations(t *testing.T) {
	tests := []struct {
		box   PageBox
		name  string
		wantX float64
		wantY float64
		x     float64
		y     float64
	}{
		{name: "top-left corner unrotated", box: letter(0), x: -612.0 * 226 / 144, y: 0, wantX: 0, wantY: 792},
		{name: "centre unrotated", box: letter(0), x: 0, y: 396.0 * 226 / 72, wantX: 306, wantY: 396},
		{name: "bottom-right unrotated", box: letter(0), x: 612.0 * 226 / 144, y: 792.0 * 226 / 72, wantX: 612, wantY: 0},
		{name: "offset cropbox", box: PageBox{LLX: 10, LLY: 20, URX: 622, URY: 812}, x: 0, y: 0, wantX: 316, wantY: 812},
		{name: "rotate 90 top-left", box: letter(90), x: -792.0 * 226 / 144, y: 0, wantX: 0, wantY: 0},
		{name: "rotate 90 top-right", box: letter(90), x: 792.0 * 226 / 144, y: 0, wantX: 0, wantY: 792},
		{name: "rotate 90 bottom-left", box: letter(90), x: -792.0 * 226 / 144, y: 612.0 * 226 / 72, wantX: 612, wantY: 0},
		{name: "rotate 180 top-left", box: letter(180), x: -612.0 * 226 / 144, y: 0, wantX: 612, wantY: 0},
		{name: "rotate 270 top-left", box: letter(270), x: -792.0 * 226 / 144, y: 0, wantX: 612, wantY: 792},
		{name: "rotate 270 bottom-left", box: letter(270), x: -792.0 * 226 / 144, y: 612.0 * 226 / 72, wantX: 0, wantY: 792},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := assert.New(t)
			m := NewMapper(tt.box)
			x, y := m.ToUser(tt.x, tt.y)
			a.InDelta(tt.wantX, x, 0.01)
			a.InDelta(tt.wantY, y, 0.01)
		})
	}
	a := assert.New(t)
	a.InDelta(792, letter(90).DisplayWidth(), 0)
	a.InDelta(612, letter(90).DisplayHeight(), 0)
}

func loadScene(t *testing.T, name string) *rm.Scene {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "rm", "testdata", name))
	require.NoError(t, err)
	scene, err := rm.Parse(data)
	require.NoError(t, err)
	return scene
}

func TestAnnotationsFromScene(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	scene := loadScene(t, "Color_and_tool_v3.14.4.rm")
	page := PageScene{Box: letter(0), PageID: "page-1", PageIndex: 3, Scene: scene}
	anns := Annotations(page)
	r.NotEmpty(anns)
	highlights, inks := 0, 0
	keys := map[string]bool{}
	for _, ann := range anns {
		a.False(keys[ann.Key], "duplicate key %s", ann.Key)
		keys[ann.Key] = true
		a.Equal(3, ann.PageIndex)
		a.Equal("4", ann.PageLabel)
		a.Regexp(`^00003\|000000\|\d{5}$`, ann.SortIndex)
		a.Regexp(`^#[0-9a-f]{6}$`, ann.Color)
		data := ann.ItemData("ATTACH01")
		a.Equal("annotation", data.ItemType)
		a.Equal("ATTACH01", data.ParentItem)
		var position map[string]any
		r.NoError(json.Unmarshal([]byte(data.AnnotationPosition), &position))
		a.EqualValues(3, position["pageIndex"])
		switch ann.Kind {
		case KindHighlight:
			highlights++
			a.NotEmpty(ann.Rects)
			for _, rect := range ann.Rects {
				a.Less(rect.X1, rect.X2)
				a.Less(rect.Y1, rect.Y2)
			}
			a.Contains(position, "rects")
		case KindInk:
			inks++
			a.Len(ann.Paths, 1)
			a.GreaterOrEqual(len(ann.Paths[0]), 4)
			a.Positive(ann.Width)
			a.Contains(position, "paths")
			a.Empty(data.AnnotationText)
		}
	}
	a.Equal(6, highlights)
	a.Equal(25, inks)
}

func TestHighlighterStrokesBecomeHighlights(t *testing.T) {
	a := assert.New(t)
	scene := loadScene(t, "More_color_highlight_shader_v3.15.4.2.rm")
	anns := Annotations(PageScene{Box: letter(0), PageID: "p", Scene: scene})
	highlights := 0
	for _, ann := range anns {
		if ann.Kind == KindHighlight {
			highlights++
			a.Empty(ann.Text)
			a.Len(ann.Rects, 1)
		}
	}
	a.Positive(highlights)
	a.Less(highlights, len(anns))
}

func TestAnnotatedPDF(t *testing.T) {
	a := assert.New(t)
	r := require.New(t)
	original, err := BlankPDF([]fpdf.SizeType{{Wd: 612, Ht: 792}, {Wd: 595.28, Ht: 841.89}, {Wd: 612, Ht: 792}})
	r.NoError(err)
	original, err = Rotate(original, 90, []string{"3"})
	r.NoError(err)
	boxes, err := PageBoxes(original)
	r.NoError(err)
	r.Len(boxes, 3)
	a.InDelta(612, boxes[0].Width(), 0.01)
	a.InDelta(595.28, boxes[1].Width(), 0.01)
	a.Equal(90, boxes[2].Rotate)
	a.InDelta(792, boxes[2].DisplayWidth(), 0.01)

	scene := loadScene(t, "rm2pdf_version6.rm")
	highlighted := loadScene(t, "Color_and_tool_v3.14.4.rm")
	out, err := AnnotatedPDF(original, []PageScene{
		{PageID: "a", PageIndex: 0, Scene: scene},
		{PageID: "c", PageIndex: 2, Scene: highlighted},
	})
	r.NoError(err)
	a.NotEqual(original, out)
	count, err := PageCount(out)
	r.NoError(err)
	a.Equal(3, count)
	outBoxes, err := PageBoxes(out)
	r.NoError(err)
	r.Len(outBoxes, 3)
	a.InDelta(612, outBoxes[0].Width(), 0.01)
	a.InDelta(792, outBoxes[2].DisplayWidth(), 0.01)

	unchanged, err := AnnotatedPDF(original, []PageScene{{PageID: "b", PageIndex: 1, Scene: loadScene(t, "Normal_AB.rm")}})
	r.NoError(err)
	a.Equal(original, unchanged)
}

func TestStrokeWidths(t *testing.T) {
	a := assert.New(t)
	fine := &rm.Line{ThicknessScale: 2, Tool: rm.ToolFineliner2}
	a.InDelta(3.6, StrokeWidth(fine), 0.001)
	a.InDelta(15, StrokeWidth(&rm.Line{Tool: rm.ToolHighlighter2}), 0)
	a.InDelta(1, StrokeOpacity(fine), 0)
	a.Less(StrokeOpacity(&rm.Line{Tool: rm.ToolShader}), 0.5)
}
