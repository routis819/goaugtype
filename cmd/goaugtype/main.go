package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/routiz/goaugt/pkg/analyzer"
	"golang.org/x/tools/go/analysis/singlechecker"
)

// version can be overridden at build time via -ldflags "-X main.version=..."
var version string

func isReleaseTag(v string) bool {
	if v == "" || v == "(devel)" || strings.HasSuffix(v, "+dirty") {
		return false
	}
	// Go pseudo-versions generated for untagged commits contain "-0.<14-digit-timestamp>-<12-hex-commit>"
	// or "v0.0.0-<14-digit-timestamp>-<12-hex-commit>".
	if strings.HasPrefix(v, "v0.0.0-") || strings.Contains(v, "-0.") {
		return false
	}
	return strings.HasPrefix(v, "v")
}

func formatVersion(explicitVersion string, info *debug.BuildInfo, goos, goarch string) string {
	if explicitVersion != "" {
		return fmt.Sprintf("goaugtype version %s %s/%s", explicitVersion, goos, goarch)
	}
	if info != nil {
		var rev string
		var dirty bool
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.modified":
				dirty = s.Value == "true"
			}
		}
		if !dirty && isReleaseTag(info.Main.Version) {
			return fmt.Sprintf("goaugtype version %s %s/%s", info.Main.Version, goos, goarch)
		}
		if rev != "" {
			if len(rev) > 7 {
				rev = rev[:7]
			}
			if dirty {
				rev += "+dirty"
			}
			return fmt.Sprintf("goaugtype version devel (%s) %s/%s", rev, goos, goarch)
		}
	}
	return fmt.Sprintf("goaugtype version devel %s/%s", goos, goarch)
}

func versionString() string {
	info, _ := debug.ReadBuildInfo()
	return formatVersion(version, info, runtime.GOOS, runtime.GOARCH)
}

func main() {
	flag.BoolFunc("version", "print version and exit", func(string) error {
		fmt.Println(versionString())
		os.Exit(0)
		return nil
	})
	singlechecker.Main(analyzer.Analyzer)
}
