// Command agentsurface inventories the AI agent machinery installed on this
// machine: model context protocol servers, desktop extensions, plugins,
// skills, connectors, instruction files and AI browser extensions.
//
// It reads local files. It makes no network calls, needs no account, and
// uploads nothing.
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"

	"github.com/Northbeams-Labs/agentsurface/internal/model"
	"github.com/Northbeams-Labs/agentsurface/internal/render"
	"github.com/Northbeams-Labs/agentsurface/internal/scan/browsers"
	"github.com/Northbeams-Labs/agentsurface/internal/scan/instructions"
	"github.com/Northbeams-Labs/agentsurface/internal/scan/mcpservers"
	"github.com/Northbeams-Labs/agentsurface/internal/scan/packages"
)

// version is set by the release build.
var version = "dev"

func main() {
	asJSON := flag.Bool("json", false, "print the inventory as JSON")
	noBaseline := flag.Bool("no-baseline", false, "do not read or write the local drift baseline")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("agentsurface", version)
		return
	}

	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot determine home directory:", err)
		os.Exit(2)
	}
	cwd, _ := os.Getwd()

	env := model.Env{OS: runtime.GOOS, HomeDir: home, Roots: []string{cwd}}

	scanners := []model.Scanner{
		mcpservers.New(),
		packages.New(),
		browsers.New(),
		instructions.New(),
	}

	result := model.Result{Tool: "agentsurface", Version: version, OS: env.OS}
	for _, s := range scanners {
		findings, gaps, errs := s.Scan(env)
		result.Findings = append(result.Findings, findings...)
		result.Gaps = append(result.Gaps, gaps...)
		result.Errors = append(result.Errors, errs...)
	}

	if !*noBaseline {
		drift, err := applyBaseline(home, result.Findings)
		if err != nil {
			result.Errors = append(result.Errors, model.ScanError{Scanner: "baseline", Err: err.Error()})
		}
		result.Drift = drift
	}

	if *asJSON {
		if err := render.JSON(os.Stdout, result); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		return
	}
	render.Text(os.Stdout, result)
}
