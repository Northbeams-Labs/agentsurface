// Package instructions inventories the files that silently steer an agent:
// the memory, rules and instruction files a coding assistant reads before it
// does anything. They matter because a repository someone cloned can carry
// instructions the user never wrote and never read.
//
// Everything here comes off the local disk. Nothing is fetched, no file is
// copied anywhere, and the notes on a finding are observations about the text
// of the file, never a verdict about it.
package instructions

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Northbeams-Labs/agentsurface/internal/model"
)

const (
	// maxWalkDepth is how far below a project root the walk goes. Instruction
	// files live near the top of a tree; going deeper mostly finds fixtures.
	maxWalkDepth = 10
	// maxWalkDirs bounds one root, so that a scan of a very large tree ends and
	// says it was cut short rather than running for minutes.
	maxWalkDirs = 25000
	// maxFileSize is the largest file that is opened at all.
	maxFileSize = 32 << 20
)

// errWalkBudget stops a walk that has run past maxWalkDirs.
var errWalkBudget = errors.New("walk budget spent")

type scanner struct{}

// New returns the instructions scanner.
func New() model.Scanner { return scanner{} }

func (scanner) Name() string { return "instructions" }

func (scanner) Scan(env model.Env) ([]model.Finding, []model.Gap, []model.ScanError) {
	s := &run{seen: map[string]bool{}}

	if env.HomeDir == "" {
		s.gaps = append(s.gaps, model.Gap{Area: "instructions", Reason: "no home directory was given, so user-scope instruction files were not searched"})
	} else {
		s.scanUser(env)
	}

	roots := dedupeRoots(env.Roots)
	if len(roots) == 0 {
		s.gaps = append(s.gaps, model.Gap{Area: "instructions", Reason: "no project root was given, so no project-scope instruction files were searched"})
	}
	for _, root := range roots {
		s.walkRoot(root)
	}

	s.markCompetingForms()
	sort.Slice(s.findings, func(i, j int) bool { return s.findings[i].Source < s.findings[j].Source })
	return s.findings, append(s.gaps, staticGaps(env)...), s.errs
}

// run is the state of one scan.
type run struct {
	findings []model.Finding
	gaps     []model.Gap
	errs     []model.ScanError
	// seen is the set of absolute paths already recorded, so overlapping roots
	// or a root inside the home directory cannot report a file twice.
	seen map[string]bool
}

// scanUser looks at the fixed set of user-scope paths. It never walks the home
// directory: a home directory is mostly other people's files, and walking it
// would be slow and nosy.
func (s *run) scanUser(env model.Env) {
	for _, spec := range userSpecs {
		if spec.onlyOS != "" && spec.onlyOS != env.OS {
			continue
		}
		pattern := filepath.Join(env.HomeDir, filepath.FromSlash(spec.rel))
		paths := []string{pattern}
		if strings.Contains(spec.rel, "*") {
			matches, err := filepath.Glob(pattern)
			if err != nil {
				s.errs = append(s.errs, model.ScanError{Scanner: "instructions", Path: pattern, Err: err.Error()})
				continue
			}
			paths = matches
			sort.Strings(paths)
		}
		for _, p := range paths {
			name := spec.name
			if name == "" {
				name = filepath.Base(p)
			}
			s.record(p, spec.client, name+" (user)", model.ScopeUser)
		}
	}
}

// walkRoot walks one project directory. It does not follow symbolic links, it
// does not descend into generated or third-party directories, and it stops
// after maxWalkDirs directories rather than running away.
func (s *run) walkRoot(root string) {
	dirs := 0
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			s.errs = append(s.errs, model.ScanError{Scanner: "instructions", Path: p, Err: err.Error()})
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if p != root && (dirsNotWalked[d.Name()] || depthBelow(root, p) > maxWalkDepth) {
				return fs.SkipDir
			}
			dirs++
			if dirs > maxWalkDirs {
				return errWalkBudget
			}
			return nil
		}
		// Anything that is not a plain file is left alone. That is what keeps
		// the walk inside the root: a symbolic link is not a regular file, so
		// it is never opened and never followed.
		if !d.Type().IsRegular() {
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		client, form, ok := projectMatch(rel)
		if !ok {
			return nil
		}
		label := form
		if form == rel {
			label += " (project)"
		} else {
			label += " (nested)"
		}
		s.record(p, client, label, model.ScopeProject)
		return nil
	})
	if errors.Is(err, errWalkBudget) {
		s.gaps = append(s.gaps, model.Gap{Area: "instructions", Reason: fmt.Sprintf("the walk of %s stopped after %d directories, so deeper parts of that tree were not searched", root, maxWalkDirs)})
		return
	}
	if err != nil {
		s.errs = append(s.errs, model.ScanError{Scanner: "instructions", Path: root, Err: err.Error()})
	}
}

// record inspects one candidate file and adds it to the inventory.
func (s *run) record(p, client, name string, scope model.Scope) {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	if s.seen[abs] {
		return
	}

	fi, err := os.Lstat(abs)
	if err != nil {
		if !os.IsNotExist(err) {
			s.errs = append(s.errs, model.ScanError{Scanner: "instructions", Path: abs, Err: err.Error()})
		}
		return
	}
	// A symbolic link is reported as absent rather than read, so that a link
	// cannot walk this scanner out of the tree it was asked to look at.
	if !fi.Mode().IsRegular() {
		return
	}
	if fi.Size() > maxFileSize {
		s.seen[abs] = true
		s.findings = append(s.findings, model.Finding{
			Kind:   model.KindInstructionFile,
			Name:   name,
			Client: client,
			Scope:  scope,
			Source: abs,
			Notes:  []string{fmt.Sprintf("%d bytes, larger than the %d byte read limit, so it was not opened", fi.Size(), maxFileSize)},
		})
		return
	}

	in, err := inspect(abs, fi.Size())
	if err != nil {
		s.errs = append(s.errs, model.ScanError{Scanner: "instructions", Path: abs, Err: err.Error()})
		return
	}
	s.seen[abs] = true
	if in.binary {
		// A file with null bytes in it is not text an agent was meant to read.
		// It is skipped, and the run says so in a gap rather than pretending it
		// looked at it.
		s.gaps = append(s.gaps, model.Gap{Area: "instructions", Reason: fmt.Sprintf("%s has the name of an instruction file but contains null bytes, so it was skipped as binary", abs)})
		return
	}

	s.findings = append(s.findings, model.Finding{
		Kind:   model.KindInstructionFile,
		Name:   name,
		Client: client,
		Scope:  scope,
		Source: abs,
		Digest: in.digest,
		Notes:  in.notes,
	})
}

// markCompetingForms notes when a client's older single-file form and its newer
// directory form are both present in the same directory. Which one the client
// actually reads depends on its version, and that is worth stating rather than
// guessing at.
func (s *run) markCompetingForms() {
	dirForms := map[string]bool{}
	for _, f := range s.findings {
		for _, sub := range []string{".cursor/rules", ".windsurf/rules"} {
			if strings.Contains(filepath.ToSlash(f.Source), "/"+sub+"/") {
				dirForms[dirOfForm(f.Source, sub)+"|"+sub] = true
			}
		}
	}
	legacy := map[string]string{".cursorrules": ".cursor/rules", ".windsurfrules": ".windsurf/rules"}
	for i, f := range s.findings {
		sub, ok := legacy[filepath.Base(f.Source)]
		if !ok {
			continue
		}
		if dirForms[filepath.Dir(f.Source)+"|"+sub] {
			s.findings[i].Notes = append(s.findings[i].Notes, "the newer "+sub+" form is present in the same directory")
		}
	}
}

// dirOfForm returns the directory that holds a client's rules directory.
func dirOfForm(source, sub string) string {
	slash := filepath.ToSlash(source)
	i := strings.Index(slash, "/"+sub+"/")
	if i < 0 {
		return filepath.Dir(source)
	}
	return filepath.FromSlash(slash[:i])
}

// depthBelow counts directory levels between a root and a path below it.
func depthBelow(root, p string) int {
	rel, err := filepath.Rel(root, p)
	if err != nil || rel == "." {
		return 0
	}
	return strings.Count(filepath.ToSlash(rel), "/") + 1
}

// dedupeRoots drops empty and repeated roots, and roots that sit inside another
// root, so a tree is walked once.
func dedupeRoots(roots []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range roots {
		if strings.TrimSpace(r) == "" {
			continue
		}
		abs, err := filepath.Abs(r)
		if err != nil {
			abs = r
		}
		if seen[abs] {
			continue
		}
		seen[abs] = true
		out = append(out, abs)
	}
	sort.Strings(out)
	var kept []string
	for _, r := range out {
		nested := false
		for _, k := range kept {
			if r == k || strings.HasPrefix(r, k+string(filepath.Separator)) {
				nested = true
				break
			}
		}
		if !nested {
			kept = append(kept, r)
		}
	}
	return kept
}
