package main

import (
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

func TestStartRenameRenamesFiles(t *testing.T) {
	app, dir := newTempApp(t, "cat.jpg", "dog.jpg")
	app.SetForm(wholeForm("pic_#"))
	res := app.StartRename()
	if res.Conflict != nil {
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
	if res.Conflict == nil {
		t.Fatal("expected a conflict")
	}
	if res.Conflict.Name != "same.txt" {
		t.Errorf("conflict name: %q", res.Conflict.Name)
	}
	res = app.ResolveConflict("autorename", false)
	if res.Conflict != nil {
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
	if res.Conflict == nil {
		t.Fatal("expected a conflict")
	}
	res = app.ResolveConflict("skip", true)
	if res.Conflict != nil {
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
