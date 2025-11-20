package adtsample

import (
	"strings"
)

// goaugtype: *strings.Reader | nil
type ImportedTester any

func ImportedTest() {
	t := ImportedTester(nil)

	// okay
	switch t.(type) {
	case *strings.Reader:
	case nil:
	}

	// should make error
	switch t.(type) {
	case *strings.Reader:
	}
}
