package rm

type Color uint32

const (
	ColorBlack       Color = 0
	ColorGray        Color = 1
	ColorWhite       Color = 2
	ColorYellow      Color = 3
	ColorGreen       Color = 4
	ColorPink        Color = 5
	ColorBlue        Color = 6
	ColorRed         Color = 7
	ColorGrayOverlap Color = 8
	ColorHighlight   Color = 9
	ColorGreen2      Color = 10
	ColorCyan        Color = 11
	ColorMagenta     Color = 12
	ColorYellow2     Color = 13
)

type Tool uint32

const (
	ToolPaintbrush1       Tool = 0
	ToolPencil1           Tool = 1
	ToolBallpoint1        Tool = 2
	ToolMarker1           Tool = 3
	ToolFineliner1        Tool = 4
	ToolHighlighter1      Tool = 5
	ToolEraser            Tool = 6
	ToolMechanicalPencil1 Tool = 7
	ToolEraserArea        Tool = 8
	ToolPaintbrush2       Tool = 12
	ToolMechanicalPencil2 Tool = 13
	ToolPencil2           Tool = 14
	ToolBallpoint2        Tool = 15
	ToolMarker2           Tool = 16
	ToolFineliner2        Tool = 17
	ToolHighlighter2      Tool = 18
	ToolCalligraphy       Tool = 21
	ToolShader            Tool = 23
)

var palette = map[Color]RGBA{
	ColorBlack:       {R: 0, G: 0, B: 0, A: 255},
	ColorBlue:        {R: 78, G: 105, B: 201, A: 255},
	ColorCyan:        {R: 139, G: 208, B: 229, A: 255},
	ColorGray:        {R: 144, G: 144, B: 144, A: 255},
	ColorGrayOverlap: {R: 125, G: 125, B: 125, A: 255},
	ColorGreen:       {R: 0, G: 255, B: 0, A: 255},
	ColorGreen2:      {R: 161, G: 216, B: 125, A: 255},
	ColorHighlight:   {R: 251, G: 247, B: 25, A: 255},
	ColorMagenta:     {R: 183, G: 130, B: 205, A: 255},
	ColorPink:        {R: 255, G: 192, B: 203, A: 255},
	ColorRed:         {R: 179, G: 62, B: 57, A: 255},
	ColorWhite:       {R: 255, G: 255, B: 255, A: 255},
	ColorYellow:      {R: 251, G: 247, B: 25, A: 255},
	ColorYellow2:     {R: 247, G: 232, B: 81, A: 255},
}

var toolNames = map[Tool]string{
	ToolBallpoint1:        "ballpoint",
	ToolBallpoint2:        "ballpoint",
	ToolCalligraphy:       "calligraphy",
	ToolEraser:            "eraser",
	ToolEraserArea:        "eraseArea",
	ToolFineliner1:        "fineliner",
	ToolFineliner2:        "fineliner",
	ToolHighlighter1:      "highlighter",
	ToolHighlighter2:      "highlighter",
	ToolMarker1:           "marker",
	ToolMarker2:           "marker",
	ToolMechanicalPencil1: "mechanicalPencil",
	ToolMechanicalPencil2: "mechanicalPencil",
	ToolPaintbrush1:       "brush",
	ToolPaintbrush2:       "brush",
	ToolPencil1:           "pencil",
	ToolPencil2:           "pencil",
	ToolShader:            "shader",
}

func (t Tool) String() string {
	if name, ok := toolNames[t]; ok {
		return name
	}
	return "unknown"
}

func (t Tool) IsEraser() bool { return t == ToolEraser || t == ToolEraserArea }

func (t Tool) IsHighlighter() bool { return t == ToolHighlighter1 || t == ToolHighlighter2 }

func (t Tool) IsShader() bool { return t == ToolShader }

func (c Color) RGBA() RGBA {
	if rgba, ok := palette[c]; ok {
		return rgba
	}
	return palette[ColorBlack]
}

func (l *Line) EffectiveColor() RGBA {
	if l.RGBA != nil {
		return *l.RGBA
	}
	return l.Color.RGBA()
}

func (g *GlyphRange) EffectiveColor() RGBA {
	if g.RGBA != nil {
		return *g.RGBA
	}
	return g.Color.RGBA()
}
