package main

import (
	"fmt"
	"log"
	"os"

	"golang.org/x/tools/go/loader"
	"golang.org/x/tools/go/packages"
)

func parseArgs() []string {
	if len(os.Args) < 2 {
		// TODO: Switch this to use golang.org/x/tools/go/packages.
		log.Fatalf("Usage: goaugt <args>\n%s", loader.FromArgsUsage)
	}
	return os.Args[1:]
}

func loadPkgs(pkgpaths []string) []*packages.Package {
	cfg := &packages.Config{Mode: pkgLoadMode}
	pkgs, err := packages.Load(cfg, pkgpaths...)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	if packages.PrintErrors(pkgs) != 0 {
		os.Exit(1)
	}
	return pkgs
}

func checkPkgs(pkgs []*packages.Package) {
	for _, pkg := range pkgs {
		checkPkg(pkg)
	}
}

func main() {
	pkgpaths := parseArgs()
	pkgs := loadPkgs(pkgpaths)
	checkPkgs(pkgs)
}
