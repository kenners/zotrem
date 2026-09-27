package render

import (
	"bytes"
	"sync"

	"codeberg.org/go-pdf/fpdf"
	"github.com/cockroachdb/errors"
	"github.com/kenners/zotrem/pkg/rm"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

var configOnce sync.Once

func pdfConfig() *model.Configuration {
	configOnce.Do(api.DisableConfigDir)
	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	return conf
}

func PageBoxes(pdf []byte) ([]PageBox, error) {
	boundaries, err := api.Boxes(bytes.NewReader(pdf), nil, pdfConfig())
	if err != nil {
		return nil, errors.Wrap(err, "read pdf page boxes")
	}
	out := make([]PageBox, 0, len(boundaries))
	for _, pb := range boundaries {
		crop := pb.CropBox()
		if crop == nil {
			return nil, errors.New("pdf page without media box")
		}
		out = append(out, PageBox{LLX: crop.LL.X, LLY: crop.LL.Y, Rotate: pb.Rot, URX: crop.UR.X, URY: crop.UR.Y})
	}
	return out, nil
}

func PageCount(pdf []byte) (int, error) {
	count, err := api.PageCount(bytes.NewReader(pdf), pdfConfig())
	if err != nil {
		return 0, errors.Wrap(err, "count pdf pages")
	}
	return count, nil
}

func Overlay(boxes []PageBox, pages []PageScene) ([]byte, bool, error) {
	byPage := map[int]PageScene{}
	for _, p := range pages {
		if p.Scene != nil && p.PageIndex >= 0 && p.PageIndex < len(boxes) {
			byPage[p.PageIndex] = p
		}
	}
	doc := fpdf.NewCustom(&fpdf.InitType{UnitStr: "pt", Size: fpdf.SizeType{Wd: 1, Ht: 1}})
	doc.SetLineCapStyle("round")
	doc.SetLineJoinStyle("round")
	drawn := false
	for i, box := range boxes {
		doc.AddPageFormat("P", fpdf.SizeType{Wd: box.DisplayWidth(), Ht: box.DisplayHeight()})
		page, ok := byPage[i]
		if !ok {
			continue
		}
		mapper := NewMapper(box)
		for _, layer := range page.Scene.Layers() {
			if !layer.Visible {
				continue
			}
			for _, glyph := range layer.Highlights {
				drawGlyph(doc, mapper, glyph)
				drawn = true
			}
			for _, line := range layer.Lines {
				if drawLine(doc, mapper, line) {
					drawn = true
				}
			}
		}
	}
	var buf bytes.Buffer
	if err := doc.Output(&buf); err != nil {
		return nil, false, errors.Wrap(err, "write overlay pdf")
	}
	return buf.Bytes(), drawn, nil
}

func drawGlyph(doc *fpdf.Fpdf, mapper Mapper, glyph *rm.GlyphRange) {
	color := glyph.EffectiveColor()
	doc.SetAlpha(0.4, "Multiply")
	doc.SetFillColor(int(color.R), int(color.G), int(color.B))
	for _, r := range glyph.Rectangles {
		u, v := mapper.ToDisplay(r.X, r.Y)
		doc.Rect(u, v, mapper.Length(r.W), mapper.Length(r.H), "F")
	}
	doc.SetAlpha(1, "Normal")
}

func drawLine(doc *fpdf.Fpdf, mapper Mapper, line *rm.Line) bool {
	if line.Tool.IsEraser() || len(line.Points) == 0 {
		return false
	}
	color := line.EffectiveColor()
	doc.SetDrawColor(int(color.R), int(color.G), int(color.B))
	doc.SetLineWidth(mapper.Length(StrokeWidth(line)))
	opacity := StrokeOpacity(line)
	blend := "Normal"
	if line.Tool.IsHighlighter() || line.Tool.IsShader() {
		blend = "Multiply"
	}
	doc.SetAlpha(opacity, blend)
	u, v := mapper.ToDisplay(line.Points[0].X, line.Points[0].Y)
	doc.MoveTo(u, v)
	if len(line.Points) == 1 {
		doc.LineTo(u+0.1, v)
	}
	for _, p := range line.Points[1:] {
		u, v = mapper.ToDisplay(p.X, p.Y)
		doc.LineTo(u, v)
	}
	doc.DrawPath("D")
	doc.SetAlpha(1, "Normal")
	return true
}

func AnnotatedPDF(original []byte, pages []PageScene) ([]byte, error) {
	boxes, err := PageBoxes(original)
	if err != nil {
		return nil, err
	}
	for i := range pages {
		if pages[i].PageIndex >= 0 && pages[i].PageIndex < len(boxes) {
			pages[i].Box = boxes[pages[i].PageIndex]
		}
	}
	overlay, drawn, err := Overlay(boxes, pages)
	if err != nil {
		return nil, err
	}
	if !drawn {
		return original, nil
	}
	wm, err := api.PDFWatermarkForReadSeeker(bytes.NewReader(overlay), 0, "pos:c, scale:1 abs, rot:0", true, false, types.POINTS)
	if err != nil {
		return nil, errors.Wrap(err, "prepare stamp")
	}
	var out bytes.Buffer
	if err := api.AddWatermarks(bytes.NewReader(original), &out, nil, wm, pdfConfig()); err != nil {
		return nil, errors.Wrap(err, "stamp annotations onto pdf")
	}
	return out.Bytes(), nil
}

func Rotate(pdf []byte, rotation int, pages []string) ([]byte, error) {
	var out bytes.Buffer
	if err := api.Rotate(bytes.NewReader(pdf), &out, rotation, pages, pdfConfig()); err != nil {
		return nil, errors.Wrap(err, "rotate pdf")
	}
	return out.Bytes(), nil
}

func BlankPDF(sizes []fpdf.SizeType) ([]byte, error) {
	doc := fpdf.NewCustom(&fpdf.InitType{UnitStr: "pt", Size: fpdf.SizeType{Wd: 1, Ht: 1}})
	doc.SetFont("Helvetica", "", 12)
	for i, size := range sizes {
		doc.AddPageFormat("P", size)
		doc.Text(72, 72, "Page "+string(rune('1'+i)))
	}
	var buf bytes.Buffer
	if err := doc.Output(&buf); err != nil {
		return nil, errors.Wrap(err, "write blank pdf")
	}
	return buf.Bytes(), nil
}
