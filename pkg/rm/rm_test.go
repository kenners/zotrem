package rm

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func load(t *testing.T, name string) *Scene {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	scene, err := Parse(data)
	require.NoError(t, err)
	return scene
}

func TestParseFixtures(t *testing.T) {
	tests := []struct {
		file       string
		highlights int
		layers     int
		lines      int
		paper      *[2]uint32
		tools      []string
		xMax       float64
		xMin       float64
	}{
		{file: "Color_and_tool_v3.14.4.rm", highlights: 6, layers: 1, lines: 25, paper: &[2]uint32{1620, 2160}, tools: []string{"ballpoint", "shader"}, xMin: 11.4, xMax: 682.8},
		{file: "Lines_v2.rm", layers: 1, lines: 10, tools: []string{"ballpoint"}, xMin: -532.2, xMax: -176.8},
		{file: "More_color_highlight_shader_v3.15.4.2.rm", layers: 1, lines: 23, paper: &[2]uint32{1620, 2160}, xMin: -423.6, xMax: 362.9},
		{file: "Normal_A_stroke_2_layers_v3.3.2.rm", layers: 2, lines: 9, tools: []string{"fineliner"}, xMin: -16.3, xMax: 221.5},
		{file: "Wikipedia_highlighted_p1.rm", highlights: 4, layers: 1},
		{file: "Wikipedia_highlighted_p2.rm", highlights: 2, layers: 1},
		{file: "With_SceneInfo_Block.rm", layers: 1, lines: 13, tools: []string{"ballpoint"}, xMin: -45.3, xMax: 197.6},
		{file: "rm2pdf_version6.rm", layers: 2, lines: 26, tools: []string{"fineliner"}, xMin: 55.5, xMax: 442.1},
		{file: "test-crdt-ordering.rm", layers: 1, paper: &[2]uint32{1404, 1872}},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			a := assert.New(t)
			scene := load(t, tt.file)
			a.Empty(scene.Warnings)
			a.Len(scene.Layers(), tt.layers)
			a.Len(scene.Lines(), tt.lines)
			a.Len(scene.Highlights(), tt.highlights)
			if tt.paper != nil {
				a.NotNil(scene.Info)
				a.Equal(tt.paper, scene.Info.PaperSize)
			}
			if tt.lines > 0 {
				xMin, xMax := 1e9, -1e9
				tools := map[string]bool{}
				for _, line := range scene.Lines() {
					tools[line.Tool.String()] = true
					for _, p := range line.Points {
						xMin = min(xMin, p.X)
						xMax = max(xMax, p.X)
					}
				}
				a.InDelta(tt.xMin, xMin, 0.1)
				a.InDelta(tt.xMax, xMax, 0.1)
				for _, tool := range tt.tools {
					a.True(tools[tool], "expected tool %s in %v", tool, tools)
				}
			}
		})
	}
}

func TestGlyphRanges(t *testing.T) {
	a := assert.New(t)
	scene := load(t, "Color_and_tool_v3.14.4.rm")
	highlights := scene.Highlights()
	a.Len(highlights, 6)
	first := highlights[0]
	a.Equal("This is a test sentence.", first.Text)
	a.Equal(ColorHighlight, first.Color)
	a.NotNil(first.RGBA)
	a.Equal("#ffed75", first.RGBA.Hex())
	a.Len(first.Rectangles, 1)
	a.InDelta(-745.6, first.Rectangles[0].X, 0.1)
	a.InDelta(216.3, first.Rectangles[0].Y, 0.1)
	a.InDelta(367.3, first.Rectangles[0].W, 0.1)
	a.InDelta(51.9, first.Rectangles[0].H, 0.1)
}

func TestLegacyGlyphRange(t *testing.T) {
	a := assert.New(t)
	scene := load(t, "Wikipedia_highlighted_p1.rm")
	highlights := scene.Highlights()
	a.Len(highlights, 4)
	a.Equal("The reMarkable uses electronic paper", highlights[0].Text)
	a.Equal(ColorYellow, highlights[0].Color)
	a.Nil(highlights[0].RGBA)
	a.Equal("#fbf719", highlights[0].EffectiveColor().Hex())
	a.NotNil(highlights[0].Start)
	a.Equal(uint32(821), *highlights[0].Start)
}

func TestLayerNamesAndVisibility(t *testing.T) {
	a := assert.New(t)
	scene := load(t, "rm2pdf_version6.rm")
	layers := scene.Layers()
	a.Len(layers, 2)
	for _, layer := range layers {
		a.NotEmpty(layer.Name)
		a.True(layer.Visible)
	}
}

func TestLineColors(t *testing.T) {
	a := assert.New(t)
	scene := load(t, "More_color_highlight_shader_v3.15.4.2.rm")
	highlighter, shader, plain := 0, 0, 0
	for _, line := range scene.Lines() {
		switch {
		case line.Tool.IsHighlighter():
			highlighter++
			a.NotNil(line.RGBA)
		case line.Tool.IsShader():
			shader++
		default:
			plain++
			a.Equal(2.0, line.ThicknessScale)
		}
	}
	a.Positive(highlighter)
	a.Positive(shader)
	a.Positive(plain)
}

func TestParseRejectsGarbage(t *testing.T) {
	_, err := Parse([]byte("not a remarkable file"))
	assert.Error(t, err)
	_, err = Parse([]byte(headerV6 + "\x10\x00\x00\x00"))
	assert.Error(t, err)
}
