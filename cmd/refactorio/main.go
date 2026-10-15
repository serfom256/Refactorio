package main

import (
	"fmt"
	"os"
	"time"

	"github.com/serfom256/refactorio/internal/cli"
	_ "github.com/serfom256/refactorio/internal/op/cleancomments"
	_ "github.com/serfom256/refactorio/internal/op/doctor"
	_ "github.com/serfom256/refactorio/internal/op/unified"
	"github.com/serfom256/refactorio/internal/stats"
)

func main() {
	cli.SetObserver(func(args *cli.Args, dur time.Duration, err error) {
		if args.Subcommand == "stats" {
			return
		}
		root, _ := args.Get("root")
		if root == "" {
			root, _ = os.Getwd()
		}
		stats.Record(args.Subcommand, args.Keys(), stats.ProjectRoot(root), dur, err)
	})

	if err := cli.Dispatch(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
