package main

import (
	"fmt"
	"os"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/cli"
)

func main() {
	if e := cli.Main(os.Args[1:], os.Stdout, os.Stderr); e != nil {
		ce, ok := e.(*core.Error)
		if !ok {
			ce = &core.Error{Code: "CLI_ERROR", Message: e.Error()}
		}
		fmt.Fprintln(os.Stderr, core.JSON(map[string]any{"error": ce}))
		os.Exit(1)
	}
}
