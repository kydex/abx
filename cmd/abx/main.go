package main

import (
	"context"
	"os"

	"github.com/kydex/abx/internal/app"
	"github.com/kydex/abx/internal/bwrap"
)

func main() {
	os.Exit(app.Run(context.Background(), os.Args[1:], bwrap.Stdio{In: os.Stdin, Out: os.Stdout, Err: os.Stderr}))
}
