package adtsample

import (
	"strings"
)

// goaugtype: *strings.Reader | nil
type ImportedTester any

func noop(v any) {}

func ImportedTest() {
	t := ImportedTester(nil)

	// okay
	switch t.(type) {
	case *strings.Reader:
	case nil:
	}

	// okay
	switch v := t.(type) {
	case *strings.Reader:
		noop(v)
	case nil:
		noop(v)
	}

	// should make error
	switch t.(type) {
	case *strings.Reader:
	}

	// should make error
	switch v := t.(type) {
	case *strings.Reader:
		noop(v)
	}
}
