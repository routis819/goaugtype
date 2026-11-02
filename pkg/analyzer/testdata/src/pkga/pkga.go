package pkga

type Alpha struct{}
type Beta struct{}
type Gamma struct{}

//goaugtype:sum *Alpha | *Beta
type ExportedSum any // want ExportedSum:".*"
