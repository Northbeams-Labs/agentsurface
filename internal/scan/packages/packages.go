// Package packages is a stub. See the package owner's ticket before filling it in.
package packages

import "github.com/Northbeams-Labs/agentsurface/internal/model"

type scanner struct{}

// New returns the packages scanner.
func New() model.Scanner { return scanner{} }

func (scanner) Name() string { return "packages" }

func (scanner) Scan(env model.Env) ([]model.Finding, []model.Gap, []model.ScanError) {
	return nil, []model.Gap{{Area: "packages", Reason: "not implemented yet"}}, nil
}
