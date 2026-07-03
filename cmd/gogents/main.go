package main

import (
	"os"

	"github.com/dccoding/gogents/internal/cli"
)

func main() {
	os.Exit(cli.Dispatch(os.Args[1:]))
}
