//! The rename engine. Pure logic, no GUI, no file system.
//!
//! The four rules mirror the four pages of the GUI. Documented behavior:
//!
//! - Letter numbering starts at `a` (or `A`). The start value offsets the
//!   sequence, so a start of 3 begins at `c`. After `z` comes `aa`, `ab`,
//!   and so on. Zero padding applies to numbers only.
//! - The prefix, suffix, insert, and delete operations act on the name
//!   without the extension. The extension stays unchanged on that page.
//! - The replace rule acts on the whole file name, extension included.
//! - Automatic conflict rename appends ` (2)`, ` (3)`, and so on before the
//!   extension.
//! - Character positions count Unicode characters, not bytes.

pub use regex::Regex;

/// Case conversion applied after the rename, from the GUI combo box.
#[derive(Clone, Copy, PartialEq, Eq, Debug, Default)]
pub enum NameCase {
    #[default]
    Unchanged,
    NameLower,
    ExtLower,
    BothLower,
    NameUpper,
    ExtUpper,
    BothUpper,
}

/// Case of the letters used by letter numbering.
#[derive(Clone, Copy, PartialEq, Eq, Debug, Default)]
pub enum LetterCase {
    #[default]
    Lower,
    Upper,
}

/// Page 1. Build the new name from a template.
///
/// `*` inserts the original name, `#` inserts the serial.
#[derive(Clone, Debug, Default)]
pub struct PatternRule {
    pub pattern: String,
    pub start: i64,
    pub step: i64,
    /// Digit count of the serial.
    pub width: usize,
    /// Pad the serial with leading zeros up to `width`.
    pub pad: bool,
    /// Use letters (a, b, c) instead of digits.
    pub letters: bool,
    pub letter_case: LetterCase,
    /// New extension without the dot, when set.
    pub extension: Option<String>,
    pub name_case: NameCase,
}

/// Page 2. Replace a string in the whole file name.
#[derive(Clone, Debug, Default)]
pub struct ReplaceRule {
    pub from: String,
    pub to: String,
    pub name_case: NameCase,
}

/// Page 4. Replace a regular expression match in the whole file name.
/// The replacement can use `$1` and `${name}` group references.
#[derive(Clone, Debug)]
pub struct RegexRule {
    pub pattern: regex::Regex,
    pub replacement: String,
    pub name_case: NameCase,
}

/// Page 3. Add and delete text. Acts on the name without the extension.
#[derive(Clone, Debug, Default)]
pub struct AddDeleteRule {
    pub prefix: String,
    pub suffix: String,
    /// Insert a string before character N (1-based) of the name.
    pub insert_at: Option<(usize, String)>,
    /// Delete every occurrence of this string.
    pub delete_text: String,
    /// Delete `count` characters starting at character `start` (1-based).
    pub delete_range: Option<(usize, usize)>,
    pub name_case: NameCase,
}

/// One rename rule, matching the four rule pages of the GUI.
#[derive(Clone, Debug)]
pub enum Rule {
    Pattern(PatternRule),
    Replace(ReplaceRule),
    AddDelete(AddDeleteRule),
    Regex(RegexRule),
}

/// What to do when the target name already exists.
#[derive(Clone, Copy, PartialEq, Eq, Debug, Default)]
pub enum ConflictPolicy {
    #[default]
    Ask,
    Overwrite,
    Skip,
    AutoRename,
}

/// Split a file name into stem and extension. The dot is not in either part.
/// A leading dot does not start an extension, so `.gitignore` is one stem.
pub fn split_name(name: &str) -> (&str, &str) {
    match name.rfind('.') {
        Some(0) | None => (name, ""),
        Some(i) => (&name[..i], &name[i + 1..]),
    }
}

/// Number to letter sequence: 1 -> a, 26 -> z, 27 -> aa.
fn letters(n: i64, case: LetterCase) -> String {
    let mut x = if n < 1 { 1 } else { n };
    let mut buf = Vec::new();
    while x > 0 {
        x -= 1;
        buf.push((x % 26) as u8);
        x /= 26;
    }
    buf.reverse();
    let base = match case {
        LetterCase::Lower => b'a',
        LetterCase::Upper => b'A',
    };
    buf.iter().map(|d| (base + d) as char).collect()
}

/// Format the serial for file `index` (0-based position in the list).
pub fn serial(rule: &PatternRule, index: usize) -> String {
    let value = rule.start + rule.step * index as i64;
    if rule.letters {
        return letters(value, rule.letter_case);
    }
    let s = value.to_string();
    if rule.pad && s.len() < rule.width {
        let zeros = rule.width - s.len();
        if value < 0 {
            format!("-{}{}", "0".repeat(zeros), &s[1..])
        } else {
            format!("{}{}", "0".repeat(zeros), s)
        }
    } else {
        s
    }
}

/// Apply the case option to a full file name.
pub fn apply_case(name: &str, case: NameCase) -> String {
    let (stem, ext) = split_name(name);
    let (stem, ext) = match case {
        NameCase::Unchanged => (stem.to_string(), ext.to_string()),
        NameCase::NameLower => (stem.to_lowercase(), ext.to_string()),
        NameCase::ExtLower => (stem.to_string(), ext.to_lowercase()),
        NameCase::BothLower => (stem.to_lowercase(), ext.to_lowercase()),
        NameCase::NameUpper => (stem.to_uppercase(), ext.to_string()),
        NameCase::ExtUpper => (stem.to_string(), ext.to_uppercase()),
        NameCase::BothUpper => (stem.to_uppercase(), ext.to_uppercase()),
    };
    if ext.is_empty() {
        stem
    } else {
        format!("{stem}.{ext}")
    }
}

fn join(stem: &str, ext: &str) -> String {
    if ext.is_empty() {
        stem.to_string()
    } else {
        format!("{stem}.{ext}")
    }
}

/// Insert `text` before 1-based character position `pos` of `s`.
/// A position past the end appends.
fn insert_at_chars(s: &str, pos: usize, text: &str) -> String {
    let pos = pos.max(1) - 1;
    let byte = s
        .char_indices()
        .nth(pos)
        .map(|(i, _)| i)
        .unwrap_or(s.len());
    format!("{}{}{}", &s[..byte], text, &s[byte..])
}

/// Delete `count` characters starting at 1-based position `start` of `s`.
fn delete_range_chars(s: &str, start: usize, count: usize) -> String {
    let start = start.max(1) - 1;
    let from = s.char_indices().nth(start).map(|(i, _)| i).unwrap_or(s.len());
    let to = s
        .char_indices()
        .nth(start + count)
        .map(|(i, _)| i)
        .unwrap_or(s.len());
    format!("{}{}", &s[..from], &s[to..])
}

/// Compute the new name of one file. `index` is the 0-based position of the
/// file in the list. Only the pattern rule uses it.
pub fn apply(rule: &Rule, name: &str, index: usize) -> String {
    match rule {
        Rule::Pattern(r) => {
            let (stem, ext) = split_name(name);
            let mut out = r.pattern.replace('*', stem);
            out = out.replace('#', &serial(r, index));
            let ext = match &r.extension {
                Some(e) => e.trim_start_matches('.').to_string(),
                None => ext.to_string(),
            };
            apply_case(&join(&out, &ext), r.name_case)
        }
        Rule::Replace(r) => {
            if r.from.is_empty() {
                return name.to_string();
            }
            apply_case(&name.replace(&r.from, &r.to), r.name_case)
        }
        Rule::AddDelete(r) => {
            let (stem, ext) = split_name(name);
            let mut s = format!("{}{}{}", r.prefix, stem, r.suffix);
            if let Some((pos, text)) = &r.insert_at {
                s = insert_at_chars(&s, *pos, text);
            }
            if !r.delete_text.is_empty() {
                s = s.replace(&r.delete_text, "");
            }
            if let Some((start, count)) = r.delete_range {
                s = delete_range_chars(&s, start, count);
            }
            apply_case(&join(&s, ext), r.name_case)
        }
        Rule::Regex(r) => {
            let out = r.pattern.replace_all(name, r.replacement.as_str());
            apply_case(&out, r.name_case)
        }
    }
}

/// Build a name that does not collide, for the AutoRename policy.
/// Appends " (2)", " (3)", and so on before the extension.
pub fn auto_rename(name: &str, exists: impl Fn(&str) -> bool) -> Option<String> {
    let (stem, ext) = split_name(name);
    for n in 2..10_000u32 {
        let candidate = join(&format!("{stem} ({n})"), ext);
        if !exists(&candidate) {
            return Some(candidate);
        }
    }
    None
}

#[cfg(test)]
mod tests {
    use super::*;

    fn pattern(p: &str) -> PatternRule {
        PatternRule {
            pattern: p.into(),
            start: 1,
            step: 1,
            width: 3,
            pad: true,
            ..Default::default()
        }
    }

    #[test]
    fn split_basic() {
        assert_eq!(split_name("a.txt"), ("a", "txt"));
        assert_eq!(split_name("a.b.txt"), ("a.b", "txt"));
        assert_eq!(split_name("noext"), ("noext", ""));
        assert_eq!(split_name(".gitignore"), (".gitignore", ""));
    }

    #[test]
    fn pattern_star_and_hash() {
        let r = pattern("pic_*_#");
        assert_eq!(apply(&Rule::Pattern(r), "cat.jpg", 0), "pic_cat_001.jpg");
    }

    #[test]
    fn pattern_constant_name() {
        let r = pattern("A_#");
        assert_eq!(apply(&Rule::Pattern(r), "x.txt", 4), "A_005.txt");
    }

    #[test]
    fn serial_step_and_start() {
        let mut r = pattern("#");
        r.start = 10;
        r.step = 5;
        assert_eq!(serial(&r, 2), "020");
    }

    #[test]
    fn serial_no_pad() {
        let mut r = pattern("#");
        r.pad = false;
        assert_eq!(serial(&r, 0), "1");
    }

    #[test]
    fn serial_letters() {
        let mut r = pattern("#");
        r.letters = true;
        assert_eq!(serial(&r, 0), "a");
        assert_eq!(serial(&r, 25), "z");
        assert_eq!(serial(&r, 26), "aa");
        let mut r2 = pattern("#");
        r2.letters = true;
        r2.start = 3;
        assert_eq!(serial(&r2, 0), "c");
        let mut r3 = pattern("#");
        r3.letters = true;
        r3.letter_case = LetterCase::Upper;
        assert_eq!(serial(&r3, 1), "B");
    }

    #[test]
    fn pattern_extension() {
        let mut r = pattern("*");
        r.extension = Some("png".into());
        assert_eq!(apply(&Rule::Pattern(r), "cat.jpg", 0), "cat.png");
    }

    #[test]
    fn case_options() {
        assert_eq!(apply_case("Foo.TXT", NameCase::NameLower), "foo.TXT");
        assert_eq!(apply_case("Foo.TXT", NameCase::ExtLower), "Foo.txt");
        assert_eq!(apply_case("Foo.TXT", NameCase::BothUpper), "FOO.TXT");
        assert_eq!(apply_case("noext", NameCase::BothUpper), "NOEXT");
    }

    #[test]
    fn replace_rule() {
        let r = ReplaceRule {
            from: "old".into(),
            to: "new".into(),
            name_case: NameCase::Unchanged,
        };
        assert_eq!(apply(&Rule::Replace(r), "old_old.txt", 0), "new_new.txt");
    }

    #[test]
    fn replace_empty_from_is_noop() {
        let r = ReplaceRule::default();
        assert_eq!(apply(&Rule::Replace(r), "a.txt", 0), "a.txt");
    }

    #[test]
    fn add_prefix_suffix() {
        let r = AddDeleteRule {
            prefix: "pre_".into(),
            suffix: "_bak".into(),
            ..Default::default()
        };
        assert_eq!(apply(&Rule::AddDelete(r), "a.txt", 0), "pre_a_bak.txt");
    }

    #[test]
    fn insert_at_position() {
        let r = AddDeleteRule {
            insert_at: Some((2, "XX".into())),
            ..Default::default()
        };
        assert_eq!(apply(&Rule::AddDelete(r), "abcd.txt", 0), "aXXbcd.txt");
    }

    #[test]
    fn delete_text_and_range() {
        let r = AddDeleteRule {
            delete_text: "tmp".into(),
            ..Default::default()
        };
        assert_eq!(apply(&Rule::AddDelete(r), "tmp_file_tmp.txt", 0), "_file_.txt");
        let r = AddDeleteRule {
            delete_range: Some((2, 3)),
            ..Default::default()
        };
        assert_eq!(apply(&Rule::AddDelete(r), "abcdef.txt", 0), "aef.txt");
    }

    #[test]
    fn unicode_positions_count_chars() {
        let r = AddDeleteRule {
            delete_range: Some((2, 2)),
            ..Default::default()
        };
        assert_eq!(apply(&Rule::AddDelete(r), "文件abc.txt", 0), "文bc.txt");
    }

    #[test]
    fn regex_groups() {
        let r = RegexRule {
            pattern: regex::Regex::new(r"(\w+)_(\d+)").unwrap(),
            replacement: "${2}_$1".into(),
            name_case: NameCase::Unchanged,
        };
        assert_eq!(apply(&Rule::Regex(r), "photo_12.jpg", 0), "12_photo.jpg");
    }

    #[test]
    fn regex_no_match_is_noop() {
        let r = RegexRule {
            pattern: regex::Regex::new(r"zzz").unwrap(),
            replacement: "x".into(),
            name_case: NameCase::Unchanged,
        };
        assert_eq!(apply(&Rule::Regex(r), "a.txt", 0), "a.txt");
    }

    #[test]
    fn auto_rename_finds_free_name() {
        let used = ["a (2).txt", "a (3).txt"];
        let n = auto_rename("a.txt", |c| used.contains(&c)).unwrap();
        assert_eq!(n, "a (4).txt");
    }
}
