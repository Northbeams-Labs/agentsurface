// Package instructions is a stub. See the package owner's ticket before filling it in.
package instructions

import "github.com/Northbeams-Labs/agentsurface/internal/model"

type scanner struct{}

// New returns the instructions scanner.
func New() model.Scanner { return scanner{} }

func (scanner) Name() string { return "instructions" }

func (scanner) Scan(env model.Env) ([]model.Finding, []model.Gap, []model.ScanError) {
	return nil, []model.Gap{{Area: "instructions", Reason: "not implemented yet"}}, nil
}
