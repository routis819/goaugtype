package pkgb

import "pkga"

func retValid() pkga.ExportedSum {
	return &pkga.Alpha{}
}

func retInvalid() pkga.ExportedSum {
	return &pkga.Gamma{} // want "invalid return: type is not permitted"
}

func takeExported(s pkga.ExportedSum) {}

func testCrossPackage() {
	var x pkga.ExportedSum

	// Valid assignments
	x = &pkga.Alpha{}
	x = &pkga.Beta{}
	x = nil

	// Invalid assignments
	x = &pkga.Gamma{} // want "invalid assigning: type is not permitted"
	x = 123           // want "invalid assigning: type is not permitted"

	// Explicit type conversion
	_ = pkga.ExportedSum(&pkga.Alpha{})
	_ = pkga.ExportedSum(&pkga.Gamma{}) // want "invalid type conversion: type is not permitted"

	// Call argument
	takeExported(&pkga.Alpha{})
	takeExported(nil)
	takeExported(&pkga.Gamma{}) // want "invalid argument: type is not permitted"

	// Type switch
	switch x.(type) {
	case *pkga.Alpha, *pkga.Beta, nil:
	}

	switch x.(type) { // want "non-exhaustive type switch. missing cases: nil"
	case *pkga.Alpha, *pkga.Beta:
	}

	switch x.(type) { // want "non-exhaustive type switch. missing cases: .*nil"
	case *pkga.Alpha:
	}
}
