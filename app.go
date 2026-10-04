package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"renamelite/internal/rename"
)

// FileRow is one entry of the file list.
type FileRow struct {
	Path   string
	Result string
}

// FormState mirrors every rule page's fields. The frontend sends the whole
// form on each change. Field names match the JSON keys the frontend uses.
type FormState struct {
	Tab string `json:"tab"`
	// Whole page.
	Pattern     string `json:"pattern"`
	Start       int64  `json:"start"`
	Step        int64  `json:"step"`
	Width       int    `json:"width"`
	Pad         bool   `json:"pad"`
	Letters     bool   `json:"letters"`
	LetterUpper bool   `json:"letterUpper"`
	ChangeExt   bool   `json:"changeExt"`
	Extension   string `json:"extension"`
	AutoResolve bool   `json:"autoResolve"`
	// The case option is shared by all pages, like the original tool.
	NameCase int `json:"nameCase"`
	// Replace page.
	From string `json:"from"`
	To   string `json:"to"`
	// Add/Delete page. InsertAt and DeleteRange enable the two optional
	// character-position operations; they have nothing to do with the
	// file extension.
	Prefix      string `json:"prefix"`
	Suffix      string `json:"suffix"`
	InsertAt    bool   `json:"insertAt"`
	InsertPos   int    `json:"insertPos"`
	InsertText  string `json:"insertText"`
	DeleteText  string `json:"deleteText"`
	DeleteRange bool   `json:"deleteRange"`
	DeleteStart int    `json:"deleteStart"`
	DeleteCount int    `json:"deleteCount"`
}

// RowView is one rendered line of the file table.
type RowView struct {
	Old     string `json:"old"`
	Preview string `json:"preview"`
	Result  string `json:"result"`
}

// RunResult reports what a rename pass did. When Conflict is set, the pass
// stopped and waits for ResolveConflict.
type RunResult struct {
	Summary  string `json:"summary"`
	Conflict string `json:"conflict,omitempty"`
}

// App holds the GUI state. The bound methods are called from JavaScript.
type App struct {
	ctx context.Context

	files         []FileRow
	form          FormState
	applyAll      rename.ConflictPolicy
	conflictIdx   int
	conflictTgt   string
	done          int
	failed        int
	unchanged     int
	processedRows int
}

// NewApp creates the application state.
func NewApp() *App {
	return &App{conflictIdx: -1}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// rule builds the active rule from the form state. An invalid regex makes
// this an error with the parse message.
func (a *App) rule() (rename.Rule, error) {
	nc := rename.CaseKind(a.form.NameCase)
	switch a.form.Tab {
	case "replace":
		return rename.Rule{Kind: rename.RuleReplace, Replace: rename.ReplaceRule{
			From: a.form.From, To: a.form.To, NameCase: nc,
		}}, nil
	case "adddelete":
		r := rename.AddDeleteRule{
			Prefix: a.form.Prefix, Suffix: a.form.Suffix,
			DeleteText: a.form.DeleteText, NameCase: nc,
		}
		if a.form.InsertAt {
			r.InsertAt = &rename.Pos{At: max(a.form.InsertPos, 1), Text: a.form.InsertText}
		}
		if a.form.DeleteRange {
			r.DeleteRange = &rename.Range{Start: max(a.form.DeleteStart, 1), Count: a.form.DeleteCount}
		}
		return rename.Rule{Kind: rename.RuleAddDelete, AddDelete: r}, nil
	case "regex":
		p, err := regexp.Compile(a.form.From)
		if err != nil {
			return rename.Rule{}, err
		}
		return rename.Rule{Kind: rename.RuleRegex, Regex: rename.RegexRule{
			Pattern: p, Replacement: a.form.To, NameCase: nc,
		}}, nil
	default: // whole
		p := rename.PatternRule{
			Pattern: a.form.Pattern, Start: a.form.Start, Step: a.form.Step,
			Width: a.form.Width, Pad: a.form.Pad, Letters: a.form.Letters,
			Extension: a.form.Extension, HasExt: a.form.ChangeExt, NameCase: nc,
		}
		if a.form.LetterUpper {
			p.LetterCase = rename.LetterUpper
		}
		return rename.Rule{Kind: rename.RulePattern, Pattern: p}, nil
	}
}

// newName computes the new name of row i from an already-built rule. It
// returns "" when the name is unchanged or empty. Building the rule is
// the expensive part (a regex compiles), so callers build it once per pass
// and loop here.
func (a *App) newName(rule rename.Rule, i int) string {
	base := filepath.Base(a.files[i].Path)
	out := rename.Apply(rule, base, i)
	if out == "" || out == base {
		return ""
	}
	return out
}

// SetForm stores the form state and returns the rendered table.
func (a *App) SetForm(f FormState) []RowView {
	a.form = f
	return a.Preview()
}

// Preview renders every row with its live preview. A rule that changes
// nothing shows the original name, so no cell is left blank. A broken rule
// (an invalid regex) shows nothing; the page already reports the error.
func (a *App) Preview() []RowView {
	rule, err := a.rule()
	views := make([]RowView, len(a.files))
	for i := range a.files {
		old := filepath.Base(a.files[i].Path)
		preview := ""
		if err == nil {
			preview = a.newName(rule, i)
			if preview == "" {
				preview = old
			}
		}
		views[i] = RowView{Old: old, Preview: preview, Result: a.files[i].Result}
	}
	return views
}

// RegexError returns the compile error of the form field, or "" when valid.
func (a *App) RegexError(expr string) string {
	if expr == "" {
		return ""
	}
	if _, err := regexp.Compile(expr); err != nil {
		return err.Error()
	}
	return ""
}

// AddFiles opens the file dialog and adds the picked files.
func (a *App) AddFiles() (int, error) {
	paths, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "RenameLite - Select Files",
	})
	if err != nil {
		return 0, err
	}
	return a.AddPaths(paths), nil
}

// AddPaths adds dropped or picked file paths to the list. It returns the
// number of files actually added. Duplicates and directories are skipped.
func (a *App) AddPaths(paths []string) int {
	added := 0
	for _, p := range paths {
		info, err := os.Stat(p)
		if err != nil || info.IsDir() {
			continue
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		// Compare the path only: a row may carry a Result from an
		// earlier run.
		if slices.ContainsFunc(a.files, func(r FileRow) bool { return r.Path == abs }) {
			continue
		}
		a.files = append(a.files, FileRow{Path: abs})
		added++
	}
	return added
}

// RemoveRow deletes the row at index i.
func (a *App) RemoveRow(i int) {
	if i >= 0 && i < len(a.files) {
		a.files = slices.Delete(a.files, i, i+1)
	}
}

// RemoveAll clears the list.
func (a *App) RemoveAll() {
	a.files = nil
}

// MoveRow moves a row to a new position. The other rows shift.
func (a *App) MoveRow(from, to int) {
	if from < 0 || to < 0 || from >= len(a.files) || to >= len(a.files) {
		return
	}
	row := a.files[from]
	a.files = slices.Delete(a.files, from, from+1)
	a.files = slices.Insert(a.files, to, row)
}

// StartRename runs the batch pass from the first row.
func (a *App) StartRename() RunResult {
	a.applyAll = rename.PolicyAsk
	a.done, a.failed, a.unchanged, a.processedRows = 0, 0, 0, 0
	return a.run(0)
}

// existsIn reports whether a name is taken in the directory of row i.
func (a *App) existsIn(i int, name string) bool {
	_, err := os.Stat(filepath.Join(filepath.Dir(a.files[i].Path), name))
	return err == nil
}

// run renames rows from startAt on. It stops at an Ask conflict. A broken
// rule changes nothing: newName never sees it because the buttons only
// offer a valid form, but guard anyway and treat every row as unchanged.
func (a *App) run(startAt int) RunResult {
	rule, err := a.rule()
	if err != nil {
		a.processedRows = len(a.files)
		a.unchanged += len(a.files) - startAt
		return RunResult{Summary: a.summaryText()}
	}
	for i := startAt; i < len(a.files); i++ {
		a.processedRows = i + 1
		newName := a.newName(rule, i)
		if newName == "" {
			a.unchanged++
			continue
		}
		if a.existsIn(i, newName) {
			base := rename.PolicyAsk
			if a.form.Tab == "whole" && a.form.AutoResolve {
				base = rename.PolicyAutoRename
			}
			policy := base
			if a.applyAll != rename.PolicyAsk {
				policy = a.applyAll
			}
			switch policy {
			case rename.PolicyAsk:
				a.conflictIdx = i
				a.conflictTgt = newName
				return RunResult{Summary: a.summaryText(), Conflict: newName}
			case rename.PolicySkip:
				a.files[i].Result = "Skipped"
				a.unchanged++
				continue
			case rename.PolicyAutoRename:
				a.renameAuto(i, newName)
				continue
			case rename.PolicyOverwrite:
				// Fall through to the plain rename below.
			}
		}
		a.renameOne(i, newName)
	}
	a.processedRows = len(a.files)
	a.conflictIdx = -1
	return RunResult{Summary: a.summaryText()}
}

func (a *App) renameOne(i int, newName string) {
	target := filepath.Join(filepath.Dir(a.files[i].Path), newName)
	if err := os.Rename(a.files[i].Path, target); err != nil {
		a.files[i].Result = fmt.Sprintf("Failed: %v", err)
		a.failed++
		return
	}
	a.files[i].Path = target
	a.files[i].Result = "OK"
	a.done++
}

// renameAuto renames row i to a free variant of want. It records a failure
// when no free name exists.
func (a *App) renameAuto(i int, want string) {
	free := rename.AutoRename(want, func(c string) bool { return a.existsIn(i, c) })
	if free == "" {
		a.files[i].Result = "Cannot rename automatically"
		a.failed++
		return
	}
	a.renameOne(i, free)
}

func (a *App) summaryText() string {
	return fmt.Sprintf("Files processed: %d\nRenamed:       %d\nFailed:        %d\nUnchanged:     %d",
		a.processedRows, a.done, a.failed, a.unchanged)
}

// ResolveConflict answers the conflict dialog. policy is one of overwrite,
// skip, autorename, cancel.
func (a *App) ResolveConflict(policy string, all bool) RunResult {
	i := a.conflictIdx
	a.conflictIdx = -1
	if i < 0 {
		return RunResult{Summary: a.summaryText()}
	}
	if all {
		switch policy {
		case "overwrite":
			a.applyAll = rename.PolicyOverwrite
		case "skip":
			a.applyAll = rename.PolicySkip
		case "autorename":
			a.applyAll = rename.PolicyAutoRename
		}
	}
	switch policy {
	case "cancel":
		return RunResult{Summary: a.summaryText()}
	case "skip":
		a.files[i].Result = "Skipped"
		a.unchanged++
	case "overwrite":
		a.renameOne(i, a.conflictTgt)
	case "autorename":
		a.renameAuto(i, a.conflictTgt)
	}
	return a.run(i + 1)
}

// Quit closes the window.
func (a *App) Quit() {
	runtime.Quit(a.ctx)
}
