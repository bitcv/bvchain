package version

import (
	"fmt"
)

const Major = 0
const Minor = 1
const Fix = 0

var Version = fmt.Sprintf("%d.%d.%d", Major, Minor, Fix)

