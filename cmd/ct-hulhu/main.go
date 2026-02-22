package main

import (
	"fmt"
	"os"

	"github.com/TheArqsz/ct-hulhu/internal/runner"
)

func main() {
	opts := runner.ParseOptions()

	r := runner.New(opts)
	if err := r.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "[ERR] ct-hulhu: %v\n", err)
		os.Exit(1)
	}
}
