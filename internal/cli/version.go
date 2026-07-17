package cli

import (
	"fmt"

	"github.com/dccoding1118/gogents/internal/version"
)

// runVersion handles `gogents version` and `gogents --version`. It takes no
// flags: the version is human-facing identification, outside the --json
// contract that covers runtime observation (D08 §3.4, CH-011 §3.2).
func runVersion() int {
	fmt.Println(version.Get())
	return 0
}
