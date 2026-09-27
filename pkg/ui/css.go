package ui

import (
	_ "embed"

	"github.com/a-h/templ"
)

//go:embed static/inbox.css
var inboxCSS string

//go:embed static/zotrem.css
var zotremCSS string

func InlineCSS() templ.Component {
	return templ.Raw(`<style>` + inboxCSS + `</style><style>` + zotremCSS + `</style>`)
}
