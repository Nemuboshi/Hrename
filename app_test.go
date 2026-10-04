package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// newTempApp builds an App whose list holds the given file names in a fresh
// temp directory. The files exist on disk.
func newTempApp(t *testing.T, names ...string) (*App, string) {
	t.Helper()
	dir := t.TempDir()
	app := NewApp()
	for _, n := range names {
		p := filepath.Join(dir, n)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		app.files = append(app.files, FileRow{Path: p})
	}
	return app, dir
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func wholeForm(pattern string) FormState {
	return FormState{Tab: "whole", Pattern: pattern, Start: 1, Step: 1, Width: 3, Pad: true}
}

func TestSetFormPreview(t *testing.T) {
	app, _ := newTempApp(t, "cat.jpg", "dog.jpg")
	rows := app.SetForm(wholeForm("pic_#"))
	if len(rows) != 2 {
		t.Fatalf("got %d rows", len(rows))
	}
	if rows[0].Preview != "pic_001.jpg" || rows[1].Preview != "pic_002.jpg" {
		t.Errorf("previews: %q %q", rows[0].Preview, rows[1].Preview)
	}
}

func TestPreviewShowsOriginalWhenUnchanged(t *testing.T) {
	// An empty Replace form changes nothing. The preview must fall back
	// to the original name, not go blank.
	app, _ := newTempApp(t, "cat.jpg")
	rows := app.SetForm(FormState{Tab: "replace"})
	if rows[0].Preview != "cat.jpg" {
		t.Errorf("preview: %q, want the original name", rows[0].Preview)
	}
}

func TestEmptyPatternIsNoop(t *testing.T) {
	// An empty Whole pattern must not produce ".ext" names. The file is
	// previewed unchanged and the run leaves it alone.
	app, dir := newTempApp(t, "cat.jpg")
	rows := app.SetForm(FormState{Tab: "whole"})
	if rows[0].Preview != "cat.jpg" {
		t.Errorf("preview: %q", rows[0].Preview)
	}
	if res := app.StartRename(); res.Conflict != "" {
		t.Fatalf("unexpected conflict %q", res.Conflict)
	}
	names := listDir(t, dir)
	if len(names) != 1 || names[0] != "cat.jpg" {
		t.Errorf("files after run: %v", names)
	}
}

func TestStartRenameRenamesFiles(t *testing.T) {
	app, dir := newTempApp(t, "cat.jpg", "dog.jpg")
	app.SetForm(wholeForm("pic_#"))
	res := app.StartRename()
	if res.Conflict != "" {
		t.Fatal("unexpected conflict")
	}
	names := listDir(t, dir)
	want := map[string]bool{"pic_001.jpg": true, "pic_002.jpg": true}
	if len(names) != 2 {
		t.Fatalf("names: %v", names)
	}
	for _, n := range names {
		if !want[n] {
			t.Errorf("unexpected file %q", n)
		}
	}
	if app.files[0].Result != "OK" || app.files[1].Result != "OK" {
		t.Errorf("results: %q %q", app.files[0].Result, app.files[1].Result)
	}
}

func TestConflictAskThenAutoRename(t *testing.T) {
	// Both files map to the same target name. The first wins. The second
	// stops the run with an Ask conflict. Auto Rename resolves it.
	app, dir := newTempApp(t, "cat.txt", "dog.txt")
	app.SetForm(wholeForm("same"))
	res := app.StartRename()
	if res.Conflict == "" {
		t.Fatal("expected a conflict")
	}
	if res.Conflict != "same.txt" {
		t.Errorf("conflict name: %q", res.Conflict)
	}
	res = app.ResolveConflict("autorename", false)
	if res.Conflict != "" {
		t.Fatal("expected the run to finish")
	}
	names := listDir(t, dir)
	want := map[string]bool{"same.txt": true, "same (2).txt": true}
	if len(names) != 2 {
		t.Fatalf("names: %v", names)
	}
	for _, n := range names {
		if !want[n] {
			t.Errorf("unexpected file %q", n)
		}
	}
}

func TestConflictApplyAllSkip(t *testing.T) {
	app, dir := newTempApp(t, "a.txt", "b.txt", "c.txt")
	app.SetForm(wholeForm("same"))
	res := app.StartRename()
	if res.Conflict == "" {
		t.Fatal("expected a conflict")
	}
	res = app.ResolveConflict("skip", true)
	if res.Conflict != "" {
		t.Fatal("apply-all skip should finish the run")
	}
	names := listDir(t, dir)
	if len(names) != 3 {
		t.Fatalf("names: %v", names)
	}
	if app.files[2].Result != "Skipped" {
		t.Errorf("row 2 result: %q", app.files[2].Result)
	}
}

func TestMoveRowShifts(t *testing.T) {
	app, _ := newTempApp(t, "a.txt", "b.txt", "c.txt")
	app.MoveRow(2, 0)
	if filepath.Base(app.files[0].Path) != "c.txt" {
		t.Errorf("order after move: %v", app.files)
	}
}

func TestRegexInvalidRulePreviewsEmpty(t *testing.T) {
	// Go reads "([unclosed" as a character class missing its "]", so it
	// fails to compile. PCRE engines accept the same string, so the
	// engine's RE2 dialect is what the GUI validates against.
	app, _ := newTempApp(t, "a.txt")
	f := FormState{Tab: "regex", From: "([unclosed", To: "x"}
	rows := app.SetForm(f)
	if rows[0].Preview != "" {
		t.Errorf("invalid regex should preview empty, got %q", rows[0].Preview)
	}
	if msg := app.RegexError(f.From); msg == "" {
		t.Error("RegexError should report the compile failure")
	}
}

func TestRegexBackreferenceInvalidInGo(t *testing.T) {
	// RE2 has no backreferences even though Perl engines match "(a)\1".
	// The GUI must treat it as broken, not run it as a literal.
	app, _ := newTempApp(t, "a.txt")
	rows := app.SetForm(FormState{Tab: "regex", From: `(a)\1`, To: "x"})
	if rows[0].Preview != "" {
		t.Errorf("backreference should preview empty, got %q", rows[0].Preview)
	}
}

func TestRegexErrorOnEmptyIsSilent(t *testing.T) {
	if msg := (&App{}).RegexError(""); msg != "" {
		t.Errorf("empty expression should report no error, got %q", msg)
	}
	if msg := (&App{}).RegexError(`(\w+)_(\d+)`); msg != "" {
		t.Errorf("valid expression should report no error, got %q", msg)
	}
}

func TestConflictApplyAllAutoRename(t *testing.T) {
	// Apply-all autorename: the second conflict is resolved without a
	// second dialog.
	app, dir := newTempApp(t, "a.txt", "b.txt", "c.txt", "same.txt")
	app.SetForm(wholeForm("same"))
	if res := app.StartRename(); res.Conflict == "" {
		t.Fatal("expected a conflict")
	}
	res := app.ResolveConflict("autorename", true)
	if res.Conflict != "" {
		t.Fatal("apply-all autorename should finish the run")
	}
	// a, b, c each map to "same.txt"; each collision gets the next free
	// suffix without a second dialog, and the original stays.
	names := listDir(t, dir)
	if len(names) != 4 {
		t.Errorf("expected 4 files, got %v", names)
	}
	if app.files[2].Result != "OK" {
		t.Errorf("row 2 result: %q", app.files[2].Result)
	}
}

func TestAddDeleteRuleThroughUI(t *testing.T) {
	// Exercises the adddelete branch of rule(): prefix, suffix, and the
	// delete-text step.
	app, _ := newTempApp(t, "report_2024.pdf")
	f := FormState{
		Tab: "adddelete", Prefix: "FINAL_", Suffix: "_v2",
		DeleteText: "2024",
	}
	rows := app.SetForm(f)
	// stem "report_2024" -> "FINAL_report_2024_v2" -> drop "2024".
	if rows[0].Preview != "FINAL_report__v2.pdf" {
		t.Errorf("preview: %q", rows[0].Preview)
	}
}

func TestAddDeletePositionsClamped(t *testing.T) {
	// The insert/delete position options clamp raw 0 to 1. Operations
	// apply in engine order: insert first, then the range delete counts
	// characters of the already-inserted name.
	app, _ := newTempApp(t, "abcdef.txt")
	f := FormState{
		Tab:      "adddelete",
		InsertAt: true, InsertPos: 0, InsertText: "X",
		DeleteRange: true, DeleteStart: 0, DeleteCount: 2,
	}
	rows := app.SetForm(f)
	// "abcdef" -> insert "X" at 1 -> "Xabcdef" -> delete chars 1-2 ("Xa").
	if rows[0].Preview != "bcdef.txt" {
		t.Errorf("preview: %q", rows[0].Preview)
	}
}

func TestLetterUpperThroughUI(t *testing.T) {
	// Covers the LetterUpper branch that wholeForm never sets.
	app, _ := newTempApp(t, "a.txt", "b.txt")
	f := wholeForm("doc_#")
	f.Letters = true
	f.LetterUpper = true
	rows := app.SetForm(f)
	if rows[0].Preview != "doc_A.txt" || rows[1].Preview != "doc_B.txt" {
		t.Errorf("previews: %q %q", rows[0].Preview, rows[1].Preview)
	}
}

func TestWebviewDataPathUnderTemp(t *testing.T) {
	// The folder must live in the OS temp directory, never in AppData:
	// the WebView2 cache does not need to survive a run.
	p := webviewDataPath()
	if !filepath.IsAbs(p) {
		t.Errorf("path %q is not absolute", p)
	}
	if filepath.Dir(p) != filepath.Clean(os.TempDir()) {
		t.Errorf("path %q is not directly in %q", p, os.TempDir())
	}
	if filepath.Base(p) != "RenameLite.WebView2" {
		t.Errorf("folder name: %q", filepath.Base(p))
	}
}

func TestAddPathsSkipsDirsAndDupes(t *testing.T) {
	app, dir := newTempApp(t, "a.txt")
	dirEntry := t.TempDir()
	dupe := filepath.Join(dir, "a.txt")
	missing := filepath.Join(dir, "nope.txt")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	n := app.AddPaths([]string{dirEntry, dupe, missing, filepath.Join(dir, "b.txt")})
	if n != 1 {
		t.Errorf("added %d, want 1 (only b.txt)", n)
	}
	if len(app.files) != 2 {
		t.Errorf("rows: %d, want 2", len(app.files))
	}
}

func TestRemoveRowAndRemoveAll(t *testing.T) {
	app, _ := newTempApp(t, "a.txt", "b.txt", "c.txt")
	app.RemoveRow(99) // out of range: no panic, no change
	app.RemoveRow(1)
	if len(app.files) != 2 || filepath.Base(app.files[1].Path) != "c.txt" {
		t.Errorf("after remove: %v", app.files)
	}
	app.RemoveAll()
	if len(app.files) != 0 {
		t.Errorf("after clear: %v", app.files)
	}
}

func TestRegexTabRenameRun(t *testing.T) {
	app, dir := newTempApp(t, "photo_12.jpg")
	// ${2} needs the braces: Go parses $2_ as one name and expands it to
	// empty. The help text on the Regex page shows the braced form.
	app.SetForm(FormState{Tab: "regex", From: `(\w+)_(\d+)`, To: "${2}_$1"})
	rows := app.Preview()
	if rows[0].Preview != "12_photo.jpg" {
		t.Fatalf("preview: %q", rows[0].Preview)
	}
	if res := app.StartRename(); res.Conflict != "" {
		t.Fatalf("unexpected conflict")
	}
	names := listDir(t, dir)
	if len(names) != 1 || names[0] != "12_photo.jpg" {
		t.Errorf("files: %v", names)
	}
}

func TestConflictOverwriteAndCancel(t *testing.T) {
	// Overwrite path: "same.txt" wins over the existing file.
	app, _ := newTempApp(t, "a.txt", "same.txt")
	app.SetForm(wholeForm("same"))
	if res := app.StartRename(); res.Conflict == "" {
		t.Fatal("expected conflict")
	}
	res := app.ResolveConflict("overwrite", false)
	if res.Conflict != "" {
		t.Fatal("expected run to finish")
	}
	// Cancel path: the run stops, the row keeps its old state.
	app2, dir2 := newTempApp(t, "b.txt", "same.txt")
	app2.SetForm(wholeForm("same"))
	if res := app2.StartRename(); res.Conflict == "" {
		t.Fatal("expected conflict")
	}
	res = app2.ResolveConflict("cancel", false)
	if len(listDir(t, dir2)) != 2 || app2.files[0].Result != "" {
		t.Errorf("cancel changed state: %v", listDir(t, dir2))
	}
}

func TestConflictPolicyInRunApplyAllOverwrite(t *testing.T) {
	// Three files map to the same name; apply-all overwrite lets the run
	// finish without further dialogs.
	app, _ := newTempApp(t, "a.txt", "b.txt", "same.txt")
	app.SetForm(wholeForm("same"))
	if res := app.StartRename(); res.Conflict == "" {
		t.Fatal("expected conflict")
	}
	res := app.ResolveConflict("overwrite", true)
	if res.Conflict != "" {
		t.Fatal("apply-all overwrite should finish the run")
	}
	if app.files[1].Result != "OK" {
		t.Errorf("row 1 after apply-all: %q", app.files[1].Result)
	}
}

func TestAutoResolveConflictsInline(t *testing.T) {
	// The Whole page's auto-resolve checkbox must skip the dialog.
	app, dir := newTempApp(t, "same.txt", "a.txt")
	f := wholeForm("same")
	f.AutoResolve = true
	app.SetForm(f)
	res := app.StartRename()
	if res.Conflict != "" {
		t.Fatal("auto-resolve should not ask")
	}
	names := listDir(t, dir)
	if len(names) != 2 || !hasName(names, "same (2).txt") {
		t.Errorf("files: %v", names)
	}
}

func hasName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

func TestRenameFailureRecorded(t *testing.T) {
	// A row whose file vanished from disk fails cleanly.
	app, dir := newTempApp(t, "a.txt")
	if err := os.Remove(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatal(err)
	}
	app.SetForm(wholeForm("new_#"))
	app.StartRename()
	if app.failed != 1 || app.files[0].Result == "" || app.files[0].Result == "OK" {
		t.Errorf("failed=%d result=%q", app.failed, app.files[0].Result)
	}
}

func TestResolveConflictWithoutConflict(t *testing.T) {
	app, _ := newTempApp(t, "a.txt")
	res := app.ResolveConflict("skip", false)
	if res.Conflict != "" {
		t.Error("resolving with no pending conflict should return a summary")
	}
}

func TestResolveConflictUnknownPolicy(t *testing.T) {
	// A policy the switch does not know falls through: the run resumes
	// past the conflicted row without touching it.
	app, dir := newTempApp(t, "a.txt", "same.txt")
	app.SetForm(wholeForm("same"))
	if res := app.StartRename(); res.Conflict == "" {
		t.Fatal("expected conflict")
	}
	res := app.ResolveConflict("frobnicate", false)
	if res.Conflict != "" {
		t.Fatal("expected the run to continue")
	}
	if app.files[0].Result != "" {
		t.Errorf("unknown policy should leave the row alone, got %q", app.files[0].Result)
	}
	if len(listDir(t, dir)) != 2 {
		t.Errorf("unexpected renames: %v", listDir(t, dir))
	}
}

func TestStartRenameWithBrokenRule(t *testing.T) {
	// UI-wise this cannot happen (an invalid pattern previews empty and
	// nothing matches), but run() guards it: every row counts unchanged
	// and no file moves.
	app, dir := newTempApp(t, "a.txt", "b.txt")
	app.SetForm(FormState{Tab: "regex", From: "([unclosed", To: "x"})
	res := app.StartRename()
	if res.Conflict != "" {
		t.Fatal("broken rule cannot conflict")
	}
	if app.unchanged != 2 || app.done != 0 {
		t.Errorf("unchanged=%d done=%d", app.unchanged, app.done)
	}
	if names := listDir(t, dir); len(names) != 2 {
		t.Errorf("files moved: %v", names)
	}
}

func TestStartupStoresContext(t *testing.T) {
	app := NewApp()
	ctx := context.Background()
	app.startup(ctx)
	if app.ctx != ctx {
		t.Error("startup should keep the context for runtime calls")
	}
}

// TestRenameAutoGivesUp fills every candidate name in the file's own
// directory so the automatic rename has nowhere to go. Kept behind
// -short: it writes 10k files.
func TestRenameAutoGivesUp(t *testing.T) {
	if testing.Short() {
		t.Skip("writes 10k files")
	}
	// AutoRename("a.txt", ...) tries "a (2).txt" through "a (9999).txt",
	// so occupy all of them.
	names := []string{"a.txt"}
	for n := 2; n < 10000; n++ {
		names = append(names, fmt.Sprintf("a (%d).txt", n))
	}
	app, _ := newTempApp(t, names...)
	app.renameAuto(0, "a.txt")
	if app.files[0].Result != "Cannot rename automatically" || app.failed != 1 {
		t.Errorf("result=%q failed=%d", app.files[0].Result, app.failed)
	}
}

func TestMoveRowGuards(t *testing.T) {
	app, _ := newTempApp(t, "a.txt", "b.txt")
	app.MoveRow(-1, 0)
	app.MoveRow(0, 5)
	app.MoveRow(5, 0)
	app.MoveRow(9, 9)
	if filepath.Base(app.files[0].Path) != "a.txt" || len(app.files) != 2 {
		t.Errorf("bad indices must not change the list: %v", app.files)
	}
}

// TestApplyAllSentinel checks that a fresh run asks again after an earlier
// run had apply-all set. PolicyAsk must be the unset value of applyAll.
func TestApplyAllSentinel(t *testing.T) {
	app, _ := newTempApp(t, "a.txt", "b.txt", "c.txt")
	app.SetForm(wholeForm("same"))
	if res := app.StartRename(); res.Conflict == "" {
		t.Fatal("expected a conflict")
	}
	// Apply-all skip finishes this run.
	if res := app.ResolveConflict("skip", true); res.Conflict != "" {
		t.Fatal("apply-all skip should finish the run")
	}
	// A new run must ask again, not inherit the old apply-all.
	res := app.StartRename()
	if res.Conflict == "" {
		t.Fatalf("second run inherited apply-all, summary: %q", res.Summary)
	}
}
