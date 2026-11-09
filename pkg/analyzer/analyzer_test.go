package analyzer_test

import (
	"testing"

	"github.com/routis819/goaugtype/pkg/analyzer"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), analyzer.Analyzer, "simple", "imported", "typeset", "assign", "assignctx", "pkga", "pkgb")
}
