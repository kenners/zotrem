//go:build mage

package main

import (
	"os"
	"path/filepath"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

var Default = Build

const pkg = "./cmd/zotrem"

func bin() string { return filepath.Join(os.Getenv("HOME"), "go", "bin", "zotrem") }

func Build() error {
	mg.Deps(Generate)
	return sh.RunV("go", "build", "-o", bin(), pkg)
}

func Dev() error {
	mg.Deps(Generate)
	return sh.RunV("go", "tool", "air", "-c", "cmd/zotrem/.air.toml")
}

func Generate() error {
	if err := sh.RunV("go", "generate", "./pkg/db/...", "./pkg/ui/...", "./uikit/..."); err != nil {
		return err
	}
	return sh.RunV("go", "generate", "./pkg/api/...")
}

func Test() error {
	mg.Deps(Generate)
	return sh.RunV("go", "test", "./...")
}

func Vet() error {
	mg.Deps(Generate)
	return sh.RunV("go", "vet", "./...")
}
