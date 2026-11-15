package search

import (
	"fmt"

	"skillshare/internal/install"
	"skillshare/internal/plainhub"
)

// Fork-only (endgame302/skillshare): a plain GitHub skills repo as a hub. The
// index is built from the repo by internal/plainhub; loadIndex asks here first.

// loadPlainRepo is replaced in tests.
var loadPlainRepo = plainhub.Load

// loadPlainRepoIndex returns the index built from hub when hub names a plain
// GitHub repo. ok is false for every other hub, which loadIndex then reads as
// before.
func loadPlainRepoIndex(hub string, opts install.ParseOptions) (doc *indexDocument, ok bool, err error) {
	if !plainhub.Detect(hub) {
		return nil, false, nil
	}
	idx, err := loadPlainRepo(hub, opts)
	if err != nil {
		return nil, true, err
	}
	doc = &indexDocument{Skills: make([]indexSkill, 0, len(idx.Skills))}
	for _, s := range idx.Skills {
		doc.Skills = append(doc.Skills, indexSkill{Name: s.Name, Description: s.Description, Source: s.Source})
	}
	return doc, true, nil
}

// PlainRepoNote is a line for the CLI when hub is a plain repo whose index
// lists fewer skills than the repo has; otherwise it is empty. It reads the
// index the last search built and never reaches the network.
func PlainRepoNote(hub string) string {
	if !plainhub.Detect(hub) {
		return ""
	}
	idx, ok := plainhub.Peek(hub)
	if !ok || !idx.Capped() {
		return ""
	}
	return fmt.Sprintf("%s has %d skills; only the first %d are listed", idx.Repo, idx.Total, len(idx.Skills))
}
