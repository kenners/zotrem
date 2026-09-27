package main

import (
	"fmt"
	"os"

	"github.com/kenners/zotrem/pkg/api"
)

func main() {
	path := "docs/openapi.json"
	if len(os.Args) > 1 {
		path = os.Args[1]
	}
	api.Spec(path)
	fmt.Fprintf(os.Stderr, "wrote %s\n", path)
}
