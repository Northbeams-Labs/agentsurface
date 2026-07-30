// Package mcpservers is a stub. See the package owner's ticket before filling it in.
package mcpservers

import "github.com/Northbeams-Labs/agentsurface/internal/model"

type scanner struct{}

// New returns the mcpservers scanner.
func New() model.Scanner { return scanner{} }

func (scanner) Name() string { return "mcpservers" }

func (scanner) Scan(env model.Env) ([]model.Finding, []model.Gap, []model.ScanError) {
	return nil, []model.Gap{{Area: "mcpservers", Reason: "not implemented yet"}}, nil
}
