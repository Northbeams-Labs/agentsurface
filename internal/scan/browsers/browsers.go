// Package browsers is a stub. See the package owner's ticket before filling it in.
package browsers

import "github.com/Northbeams-Labs/agentsurface/internal/model"

type scanner struct{}

// New returns the browsers scanner.
func New() model.Scanner { return scanner{} }

func (scanner) Name() string { return "browsers" }

func (scanner) Scan(env model.Env) ([]model.Finding, []model.Gap, []model.ScanError) {
	return nil, []model.Gap{{Area: "browsers", Reason: "not implemented yet"}}, nil
}
