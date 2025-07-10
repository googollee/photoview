package flags

import "flag"

var Database bool
var Filesystem bool

func init() {
	flag.BoolVar(&Database, "database", false, "run database integration tests")
	flag.BoolVar(&Filesystem, "filesystem", false, "run filesystem integration tests")
}
