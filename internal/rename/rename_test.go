package rename

import (
	"regexp"
	"testing"
)

func testPattern(p string) PatternRule {
	return PatternRule{
		Pattern: p,
		Start:   1,
		Step:    1,
		Width:   3,
		Pad:     true,
	}
}

func TestSplitName(t *testing.T) {
	cases := []struct {
		in         string
		stem, want string
	}{
		{"a.txt", "a", "txt"},
		{"a.b.txt", "a.b", "txt"},
		{"noext", "noext", ""},
		{".gitignore", ".gitignore", ""},
	}
	for _, c := range cases {
		s, e := SplitName(c.in)
		if s != c.stem || e != c.want {
			t.Errorf("SplitName(%q) = (%q, %q), want (%q, %q)", c.in, s, e, c.stem, c.want)
		}
	}
}

func TestPatternStarAndHash(t *testing.T) {
	r := Rule{Kind: RulePattern, Pattern: testPattern("pic_*_#")}
	got := Apply(r, "cat.jpg", 0)
	if got != "pic_cat_001.jpg" {
		t.Errorf("got %q", got)
	}
}

func TestPatternConstantName(t *testing.T) {
	r := Rule{Kind: RulePattern, Pattern: testPattern("A_#")}
	got := Apply(r, "x.txt", 4)
	if got != "A_005.txt" {
		t.Errorf("got %q", got)
	}
}

func TestPatternEmptyIsNoop(t *testing.T) {
	// An empty template must not build ".ext" from the bare extension.
	p := testPattern("")
	p.HasExt = true
	p.Extension = "txt"
	r := Rule{Kind: RulePattern, Pattern: p}
	if got := Apply(r, "cat.jpg", 0); got != "cat.jpg" {
		t.Errorf("got %q", got)
	}
}

func TestSerialStepAndStart(t *testing.T) {
	p := testPattern("#")
	p.Start = 10
	p.Step = 5
	if got := Serial(&p, 2); got != "020" {
		t.Errorf("got %q", got)
	}
}

func TestSerialNoPad(t *testing.T) {
	p := testPattern("#")
	p.Pad = false
	if got := Serial(&p, 0); got != "1" {
		t.Errorf("got %q", got)
	}
}

func TestSerialLetters(t *testing.T) {
	p := testPattern("#")
	p.Letters = true
	if got := Serial(&p, 0); got != "a" {
		t.Errorf("index 0: got %q", got)
	}
	if got := Serial(&p, 25); got != "z" {
		t.Errorf("index 25: got %q", got)
	}
	if got := Serial(&p, 26); got != "aa" {
		t.Errorf("index 26: got %q", got)
	}
	p2 := testPattern("#")
	p2.Letters = true
	p2.Start = 3
	if got := Serial(&p2, 0); got != "c" {
		t.Errorf("start 3: got %q", got)
	}
	p3 := testPattern("#")
	p3.Letters = true
	p3.LetterCase = LetterUpper
	if got := Serial(&p3, 1); got != "B" {
		t.Errorf("upper: got %q", got)
	}
}

func TestPatternExtension(t *testing.T) {
	p := testPattern("*")
	p.Extension = "png"
	p.HasExt = true
	r := Rule{Kind: RulePattern, Pattern: p}
	if got := Apply(r, "cat.jpg", 0); got != "cat.png" {
		t.Errorf("got %q", got)
	}
}

func TestCaseOptions(t *testing.T) {
	cases := []struct {
		in   string
		kind CaseKind
		want string
	}{
		{"Foo.TXT", CaseUnchanged, "Foo.TXT"},
		{"Foo.TXT", CaseNameLower, "foo.TXT"},
		{"Foo.TXT", CaseExtLower, "Foo.txt"},
		{"Foo.TXT", CaseBothLower, "foo.txt"},
		{"Foo.TXT", CaseNameUpper, "FOO.TXT"},
		{"Foo.txt", CaseExtUpper, "Foo.TXT"},
		{"Foo.TXT", CaseBothUpper, "FOO.TXT"},
		{"noext", CaseBothUpper, "NOEXT"},
		{"noext", CaseExtLower, "noext"},
	}
	for _, c := range cases {
		if got := ApplyCase(c.in, c.kind); got != c.want {
			t.Errorf("ApplyCase(%q, %d) = %q, want %q", c.in, c.kind, got, c.want)
		}
	}
}

func TestSerialNegativePadKeepsSign(t *testing.T) {
	p := testPattern("#")
	p.Start = -5
	p.Step = 0
	// The width counts the whole number, sign included: "-5" pads to
	// three characters, not three digits.
	if got := Serial(&p, 0); got != "-05" {
		t.Errorf("got %q", got)
	}
}

func TestSerialLettersFloorAtOne(t *testing.T) {
	p := testPattern("#")
	p.Letters = true
	p.Start = 0 // below the first letter: clamps to a
	if got := Serial(&p, 0); got != "a" {
		t.Errorf("got %q", got)
	}
}

func TestInsertPastEndAppends(t *testing.T) {
	r := Rule{Kind: RuleAddDelete, AddDelete: AddDeleteRule{
		InsertAt: &Pos{At: 99, Text: "!"},
	}}
	if got := Apply(r, "a.txt", 0); got != "a!.txt" {
		t.Errorf("got %q", got)
	}
}

func TestDeleteRangePastEnd(t *testing.T) {
	r := Rule{Kind: RuleAddDelete, AddDelete: AddDeleteRule{
		DeleteRange: &Range{Start: 3, Count: 99},
	}}
	if got := Apply(r, "abcdef.txt", 0); got != "ab.txt" {
		t.Errorf("got %q", got)
	}
}

func TestAddDeleteAllOperationsCombined(t *testing.T) {
	r := Rule{Kind: RuleAddDelete, AddDelete: AddDeleteRule{
		Prefix: "P_", Suffix: "_S", DeleteText: "mid",
	}}
	if got := Apply(r, "a_midx.txt", 0); got != "P_a_x_S.txt" {
		t.Errorf("got %q", got)
	}
}

func TestAutoRenameGivesUpAtLimit(t *testing.T) {
	// Every candidate taken: the search must stop and report failure.
	got := AutoRename("a.txt", func(string) bool { return true })
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestApplyRegexKeepsNameWhenNoChange(t *testing.T) {
	// The GUI relies on Apply returning the input name untouched when a
	// rule changes nothing, so it can show the original in the preview.
	r := Rule{Kind: RuleReplace, Replace: ReplaceRule{From: "zzz", To: "y"}}
	if got := Apply(r, "a.txt", 0); got != "a.txt" {
		t.Errorf("got %q", got)
	}
}

func TestReplaceRule(t *testing.T) {
	r := Rule{Kind: RuleReplace, Replace: ReplaceRule{From: "old", To: "new"}}
	if got := Apply(r, "old_old.txt", 0); got != "new_new.txt" {
		t.Errorf("got %q", got)
	}
}

func TestReplaceEmptyFromIsNoop(t *testing.T) {
	r := Rule{Kind: RuleReplace}
	if got := Apply(r, "a.txt", 0); got != "a.txt" {
		t.Errorf("got %q", got)
	}
}

func TestAddPrefixSuffix(t *testing.T) {
	r := Rule{Kind: RuleAddDelete, AddDelete: AddDeleteRule{Prefix: "pre_", Suffix: "_bak"}}
	if got := Apply(r, "a.txt", 0); got != "pre_a_bak.txt" {
		t.Errorf("got %q", got)
	}
}

func TestInsertAtPosition(t *testing.T) {
	r := Rule{Kind: RuleAddDelete, AddDelete: AddDeleteRule{
		InsertAt: &Pos{At: 2, Text: "XX"},
	}}
	if got := Apply(r, "abcd.txt", 0); got != "aXXbcd.txt" {
		t.Errorf("got %q", got)
	}
}

func TestDeleteTextAndRange(t *testing.T) {
	r := Rule{Kind: RuleAddDelete, AddDelete: AddDeleteRule{DeleteText: "tmp"}}
	if got := Apply(r, "tmp_file_tmp.txt", 0); got != "_file_.txt" {
		t.Errorf("delete text: got %q", got)
	}
	r2 := Rule{Kind: RuleAddDelete, AddDelete: AddDeleteRule{
		DeleteRange: &Range{Start: 2, Count: 3},
	}}
	if got := Apply(r2, "abcdef.txt", 0); got != "aef.txt" {
		t.Errorf("delete range: got %q", got)
	}
}

func TestUnicodePositionsCountChars(t *testing.T) {
	r := Rule{Kind: RuleAddDelete, AddDelete: AddDeleteRule{
		DeleteRange: &Range{Start: 2, Count: 2},
	}}
	if got := Apply(r, "文件abc.txt", 0); got != "文bc.txt" {
		t.Errorf("got %q", got)
	}
}

func TestRegexGroups(t *testing.T) {
	r := Rule{Kind: RuleRegex, Regex: RegexRule{
		Pattern:     regexp.MustCompile(`(\w+)_(\d+)`),
		Replacement: "${2}_$1",
	}}
	if got := Apply(r, "photo_12.jpg", 0); got != "12_photo.jpg" {
		t.Errorf("got %q", got)
	}
}

func TestRegexNoMatchIsNoop(t *testing.T) {
	r := Rule{Kind: RuleRegex, Regex: RegexRule{
		Pattern:     regexp.MustCompile(`zzz`),
		Replacement: "x",
	}}
	if got := Apply(r, "a.txt", 0); got != "a.txt" {
		t.Errorf("got %q", got)
	}
}

func TestApplyUnknownKindReturnsName(t *testing.T) {
	// A zero Rule has no valid kind; Apply must not guess.
	r := Rule{Kind: RuleKind(99)}
	if got := Apply(r, "a.txt", 0); got != "a.txt" {
		t.Errorf("got %q", got)
	}
}

func TestPositionsBelowOneClamp(t *testing.T) {
	// The engine clamps defensively even though the GUI already sends
	// positions of at least 1.
	r := Rule{Kind: RuleAddDelete, AddDelete: AddDeleteRule{
		InsertAt:    &Pos{At: 0, Text: "X"},
		DeleteRange: &Range{Start: 0, Count: 1},
	}}
	// Insert before char 1, then delete char 1: the inserted X goes again.
	if got := Apply(r, "abc.txt", 0); got != "abc.txt" {
		t.Errorf("got %q", got)
	}
}

func TestAutoRenameFindsFreeName(t *testing.T) {
	used := map[string]bool{"a (2).txt": true, "a (3).txt": true}
	got := AutoRename("a.txt", func(c string) bool { return used[c] })
	if got != "a (4).txt" {
		t.Errorf("got %q", got)
	}
}
