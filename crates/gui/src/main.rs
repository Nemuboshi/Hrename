//! The GUI. A tab control with four rule pages on the left, the file list
//! with preview and result columns on the right, and the action buttons at
//! the bottom. The engine is the hrename-core crate and knows nothing about
//! this file.

use std::path::{Path, PathBuf};

use eframe::egui;
use hrename_core::{
    apply, auto_rename, AddDeleteRule, ConflictPolicy, LetterCase, NameCase, PatternRule, Regex,
    RegexRule, ReplaceRule, Rule,
};

fn main() -> eframe::Result<()> {
    let options = eframe::NativeOptions {
        viewport: egui::ViewportBuilder::default()
            .with_title("Batch Rename Files")
            .with_inner_size([880.0, 560.0])
            .with_resizable(false),
        ..Default::default()
    };
    eframe::run_native(
        "hrename",
        options,
        Box::new(|cc| {
            cc.egui_ctx.set_theme(egui::ThemePreference::Light);
            Ok(Box::new(App::default()))
        }),
    )
}

/// One row of the file list.
struct FileRow {
    path: PathBuf,
    result: Option<Result<String, String>>,
}

/// Which rule page is active, matching the tab order of the GUI.
#[derive(Clone, Copy, PartialEq, Eq)]
enum Tab {
    Pattern,
    Replace,
    AddDelete,
    Regex,
}

/// Form state for the pattern page. Mirrors PatternRule with form fields.
struct PatternForm {
    pattern: String,
    start: i64,
    step: i64,
    width: usize,
    pad: bool,
    letters: bool,
    letter_case: LetterCase,
    change_ext: bool,
    extension: String,
    name_case: NameCase,
    auto_resolve: bool,
}

impl Default for PatternForm {
    fn default() -> Self {
        Self {
            pattern: "*".into(),
            start: 1,
            step: 1,
            width: 1,
            pad: false,
            letters: false,
            letter_case: LetterCase::Lower,
            change_ext: false,
            extension: String::new(),
            name_case: NameCase::Unchanged,
            auto_resolve: false,
        }
    }
}

/// Form state for the replace page.
#[derive(Default)]
struct ReplaceForm {
    from: String,
    to: String,
}

/// Form state for the add and delete page.
#[derive(Default)]
struct AddDeleteForm {
    prefix: String,
    suffix: String,
    ext_add: bool,
    insert_pos: usize,
    insert_text: String,
    delete_text: String,
    ext_delete: bool,
    delete_start: usize,
    delete_count: usize,
}

/// Form state for the regex page.
#[derive(Default)]
struct RegexForm {
    from: String,
    to: String,
}

/// A pending conflict, shown in the modal dialog.
struct Conflict {
    index: usize,
    target: PathBuf,
}

struct App {
    files: Vec<FileRow>,
    selected: Option<usize>,
    tab: Tab,
    pattern: PatternForm,
    replace: ReplaceForm,
    add_delete: AddDeleteForm,
    regex: RegexForm,
    conflict: Option<Conflict>,
    apply_all: Option<ConflictPolicy>,
    summary: Option<String>,
    show_help: bool,
    renamed_once: bool,
}

impl Default for App {
    fn default() -> Self {
        Self {
            files: Vec::new(),
            selected: None,
            tab: Tab::Pattern,
            pattern: PatternForm::default(),
            replace: ReplaceForm::default(),
            add_delete: AddDeleteForm::default(),
            regex: RegexForm::default(),
            conflict: None,
            apply_all: None,
            summary: None,
            show_help: false,
            renamed_once: false,
        }
    }
}

impl App {
    /// Build the active rule from the form state. An invalid regex on the
    /// regex page makes this an Err with the parse error text.
    fn rule(&self) -> Result<Rule, String> {
        let rule = match self.tab {
            Tab::Pattern => {
                let f = &self.pattern;
                Rule::Pattern(PatternRule {
                    pattern: f.pattern.clone(),
                    start: f.start,
                    step: f.step,
                    width: f.width,
                    pad: f.pad,
                    letters: f.letters,
                    letter_case: f.letter_case,
                    extension: f.change_ext.then(|| f.extension.clone()),
                    name_case: f.name_case,
                })
            }
            Tab::Replace => {
                let f = &self.replace;
                Rule::Replace(ReplaceRule {
                    from: f.from.clone(),
                    to: f.to.clone(),
                    name_case: self.pattern.name_case,
                })
            }
            Tab::AddDelete => {
                let f = &self.add_delete;
                Rule::AddDelete(AddDeleteRule {
                    prefix: f.prefix.clone(),
                    suffix: f.suffix.clone(),
                    insert_at: f
                        .ext_add
                        .then(|| (f.insert_pos.max(1), f.insert_text.clone())),
                    delete_text: f.delete_text.clone(),
                    delete_range: f
                        .ext_delete
                        .then(|| (f.delete_start.max(1), f.delete_count)),
                    name_case: self.pattern.name_case,
                })
            }
            Tab::Regex => {
                let f = &self.regex;
                let pattern = Regex::new(&f.from).map_err(|e| e.to_string())?;
                Rule::Regex(RegexRule {
                    pattern,
                    replacement: f.to.clone(),
                    name_case: self.pattern.name_case,
                })
            }
        };
        Ok(rule)
    }

    /// The new name of the file in row `i`, or None when the name is
    /// unchanged, empty, or the rule is invalid.
    fn new_name(&self, i: usize) -> Option<String> {
        let row = &self.files[i];
        let name = row.path.file_name()?.to_str()?;
        let out = apply(&self.rule().ok()?, name, i);
        if out.is_empty() || out == name {
            None
        } else {
            Some(out)
        }
    }

    fn add_files(&mut self, paths: impl IntoIterator<Item = PathBuf>) {
        for p in paths {
            if self.files.iter().any(|r| r.path == p) {
                continue;
            }
            self.files.push(FileRow {
                path: p,
                result: None,
            });
        }
    }

    fn move_row(&mut self, from: usize, to: usize) {
        if from < self.files.len() && to < self.files.len() {
            self.files.swap(from, to);
            self.selected = Some(to);
        }
    }

    /// Rename the files in list order. Conflicts follow the chosen policy.
    /// With the Ask policy the run stops on the first conflict and the modal
    /// dialog resumes it.
    fn run(&mut self, start_at: usize) {
        let mut done = 0usize;
        let mut failed = 0usize;
        let mut unchanged = 0usize;
        let mut i = start_at;
        while i < self.files.len() {
            let Some(new_name) = self.new_name(i) else {
                unchanged += 1;
                i += 1;
                continue;
            };
            let target = self.files[i].path.with_file_name(&new_name);
            if target.exists() {
                let base = if self.tab == Tab::Pattern && self.pattern.auto_resolve {
                    ConflictPolicy::AutoRename
                } else {
                    ConflictPolicy::Ask
                };
                let policy = self.apply_all.unwrap_or(base);
                match policy {
                    ConflictPolicy::Ask => {
                        self.conflict = Some(Conflict { index: i, target });
                        self.finish_summary(done, failed, unchanged, i);
                        return;
                    }
                    ConflictPolicy::Overwrite => {}
                    ConflictPolicy::Skip => {
                        self.files[i].result = Some(Err("Skipped".into()));
                        unchanged += 1;
                        i += 1;
                        continue;
                    }
                    ConflictPolicy::AutoRename => {
                        let dir = target.parent().map(Path::to_path_buf);
                        match auto_rename(&new_name, |c| {
                            dir.as_ref().map(|d| d.join(c)).unwrap_or_default().exists()
                        }) {
                            Some(free) => {
                                self.rename_one(i, free, &mut done, &mut failed);
                                i += 1;
                                continue;
                            }
                            None => {
                                self.files[i].result =
                                    Some(Err("Cannot rename automatically".into()));
                                failed += 1;
                                i += 1;
                                continue;
                            }
                        }
                    }
                }
            }
            self.rename_one(i, new_name, &mut done, &mut failed);
            i += 1;
        }
        self.finish_summary(done, failed, unchanged, self.files.len());
    }

    fn rename_one(&mut self, i: usize, new_name: String, done: &mut usize, failed: &mut usize) {
        let target = self.files[i].path.with_file_name(&new_name);
        match std::fs::rename(&self.files[i].path, &target) {
            Ok(()) => {
                self.files[i].path = target;
                self.files[i].result = Some(Ok("OK".into()));
                *done += 1;
            }
            Err(e) => {
                self.files[i].result = Some(Err(format!("Failed: {e}")));
                *failed += 1;
            }
        }
    }

    fn finish_summary(&mut self, done: usize, failed: usize, unchanged: usize, processed: usize) {
        self.renamed_once = true;
        self.summary = Some(format!(
            "Files processed: {processed}\nRenamed:       {done}\nFailed:        {failed}\nUnchanged:     {unchanged}"
        ));
    }

    /// The user answered the conflict dialog. Resume the run.
    fn resolve_conflict(&mut self, choice: ConflictPolicy, all: bool) {
        let Some(c) = self.conflict.take() else { return };
        if all {
            self.apply_all = Some(choice);
        }
        match choice {
            ConflictPolicy::Skip => {
                self.files[c.index].result = Some(Err("Skipped".into()));
                self.run(c.index + 1);
            }
            ConflictPolicy::AutoRename => {
                let name = c
                    .target
                    .file_name()
                    .map(|s| s.to_string_lossy().into_owned())
                    .unwrap_or_default();
                let dir = c.target.parent().map(Path::to_path_buf);
                if let Some(free) = auto_rename(&name, |n| {
                    dir.as_ref().map(|d| d.join(n)).unwrap_or_default().exists()
                }) {
                    let mut d = 0;
                    let mut f = 0;
                    self.rename_one(c.index, free, &mut d, &mut f);
                }
                self.run(c.index + 1);
            }
            ConflictPolicy::Overwrite => {
                let name = c
                    .target
                    .file_name()
                    .map(|s| s.to_string_lossy().into_owned())
                    .unwrap_or_default();
                let mut d = 0;
                let mut f = 0;
                self.rename_one(c.index, name, &mut d, &mut f);
                self.run(c.index + 1);
            }
            ConflictPolicy::Ask => unreachable!(),
        }
    }
}

fn case_label(c: NameCase) -> &'static str {
    match c {
        NameCase::Unchanged => "No change",
        NameCase::NameLower => "Lowercase file name",
        NameCase::ExtLower => "Lowercase extension",
        NameCase::BothLower => "Lowercase name and extension",
        NameCase::NameUpper => "Uppercase file name",
        NameCase::ExtUpper => "Uppercase extension",
        NameCase::BothUpper => "Uppercase name and extension",
    }
}

const CASES: [NameCase; 7] = [
    NameCase::Unchanged,
    NameCase::NameLower,
    NameCase::ExtLower,
    NameCase::BothLower,
    NameCase::NameUpper,
    NameCase::ExtUpper,
    NameCase::BothUpper,
];

impl eframe::App for App {
    fn update(&mut self, ctx: &egui::Context, _frame: &mut eframe::Frame) {
        // Accept dropped files.
        let dropped: Vec<PathBuf> = ctx.input(|i| {
            i.raw.dropped_files.iter().filter_map(|f| f.path.clone()).collect()
        });
        if !dropped.is_empty() {
            self.add_files(dropped);
        }

        // Bottom row: Start, Cancel, Help.
        egui::TopBottomPanel::bottom("actions").show(ctx, |ui| {
            ui.separator();
            ui.horizontal(|ui| {
                ui.add_space(370.0);
                let start_label = if self.renamed_once {
                    "Rename Again"
                } else {
                    "Start Rename"
                };
                if ui.button(start_label).clicked() {
                    self.apply_all = None;
                    self.run(0);
                }
                if ui.button("Cancel").clicked() {
                    ctx.send_viewport_cmd(egui::ViewportCommand::Close);
                }
                if ui.button("Help").clicked() {
                    self.show_help = true;
                }
            });
        });

        // Left: the tab control with the four rule pages.
        egui::SidePanel::left("rules")
            .resizable(false)
            .exact_width(380.0)
            .show(ctx, |ui| {
                ui.horizontal(|ui| {
                    ui.selectable_value(&mut self.tab, Tab::Pattern, "Pattern");
                    ui.selectable_value(&mut self.tab, Tab::Replace, "Replace");
                    ui.selectable_value(&mut self.tab, Tab::AddDelete, "Add/Delete");
                    ui.selectable_value(&mut self.tab, Tab::Regex, "Regex");
                });
                egui::Frame::group(ui.style()).show(ui, |ui| {
                    ui.set_min_width(360.0);
                    match self.tab {
                        Tab::Pattern => pattern_page(ui, &mut self.pattern),
                        Tab::Replace => replace_page(ui, &mut self.replace),
                        Tab::AddDelete => add_delete_page(ui, &mut self.add_delete),
                        Tab::Regex => regex_page(ui, &mut self.regex),
                    }
                });
            });

        // Right: the file list group with the move buttons on its right edge
        // and the add and remove buttons below it.
        egui::CentralPanel::default().show(ctx, |ui| {
            egui::Frame::group(ui.style()).show(ui, |ui| {
                ui.label("File list");
                ui.horizontal(|ui| {
                    egui::ScrollArea::vertical()
                        .min_scrolled_height(360.0)
                        .max_height(360.0)
                        .show(ui, |ui| {
                            egui::Grid::new("file_grid").striped(true).show(ui, |ui| {
                                ui.strong("Original name");
                                ui.strong("Preview");
                                ui.strong("Result");
                                ui.end_row();
                                let mut clicked = None;
                                for i in 0..self.files.len() {
                                    let row = &self.files[i];
                                    let old = row
                                        .path
                                        .file_name()
                                        .map(|s| s.to_string_lossy().into_owned())
                                        .unwrap_or_default();
                                    let result = match &row.result {
                                        Some(Ok(s)) => s.clone(),
                                        Some(Err(s)) => s.clone(),
                                        None => String::new(),
                                    };
                                    let preview = self.new_name(i).unwrap_or_default();
                                    let sel = self.selected == Some(i);
                                    if ui.selectable_label(sel, old).clicked() {
                                        clicked = Some(i);
                                    }
                                    ui.label(preview);
                                    ui.label(result);
                                    ui.end_row();
                                }
                                if let Some(i) = clicked {
                                    self.selected = Some(i);
                                }
                            });
                        });
                    ui.vertical(|ui| {
                        ui.add_space(60.0);
                        let n = self.files.len();
                        if ui.small_button("⤒").clicked() {
                            if let Some(i) = self.selected {
                                self.move_row(i, 0);
                            }
                        }
                        if ui.small_button("↑").clicked() {
                            if let Some(i) = self.selected {
                                if i > 0 {
                                    self.move_row(i, i - 1);
                                }
                            }
                        }
                        if ui.small_button("↓").clicked() {
                            if let Some(i) = self.selected {
                                if i + 1 < n {
                                    self.move_row(i, i + 1);
                                }
                            }
                        }
                        if ui.small_button("⤓").clicked() {
                            if let Some(i) = self.selected {
                                if n > 0 {
                                    self.move_row(i, n - 1);
                                }
                            }
                        }
                    });
                });
                if self.files.is_empty() {
                    ui.label(
                        "The list is empty. You can add files in two ways:\n \
                         (1) Click the Add button to add files to rename\n \
                         (2) Drop files onto this window with the mouse",
                    );
                }
                ui.horizontal(|ui| {
                    ui.add_space(24.0);
                    if ui.button("Add").clicked() {
                        if let Some(paths) = rfd::FileDialog::new().pick_files() {
                            self.add_files(paths);
                        }
                    }
                    ui.add_space(24.0);
                    if ui.button("Remove").clicked() {
                        if let Some(i) = self.selected {
                            self.files.remove(i);
                            self.selected = None;
                        }
                    }
                    ui.add_space(24.0);
                    if ui.button("Remove All").clicked() {
                        self.files.clear();
                        self.selected = None;
                    }
                });
            });
        });

        // Conflict dialog.
        if let Some(c) = &self.conflict {
            let mut open = true;
            let mut choice = None;
            let mut apply_all = false;
            egui::Window::new("Batch Rename Files")
                .collapsible(false)
                .resizable(false)
                .open(&mut open)
                .show(ctx, |ui| {
                    let name = c
                        .target
                        .file_name()
                        .map(|s| s.to_string_lossy().into_owned())
                        .unwrap_or_default();
                    ui.label(format!("The file name \"{name}\" already exists."));
                    ui.label("Do you want to resolve this name conflict automatically?");
                    ui.label("Press Cancel to stop the batch operation.");
                    ui.checkbox(&mut apply_all, "Apply to all");
                    ui.horizontal(|ui| {
                        if ui.button("Overwrite").clicked() {
                            choice = Some(ConflictPolicy::Overwrite);
                        }
                        if ui.button("Skip").clicked() {
                            choice = Some(ConflictPolicy::Skip);
                        }
                        if ui.button("Auto Rename").clicked() {
                            choice = Some(ConflictPolicy::AutoRename);
                        }
                    });
                });
            if let Some(ch) = choice {
                self.resolve_conflict(ch, apply_all);
            }
            if !open {
                self.conflict = None;
            }
        }

        // Summary dialog at the end of a run.
        if let Some(text) = &self.summary {
            let mut open = true;
            egui::Window::new("Batch Rename Files")
                .collapsible(false)
                .resizable(false)
                .open(&mut open)
                .show(ctx, |ui| {
                    ui.label(text);
                });
            if !open {
                self.summary = None;
            }
        }

        // Help dialog.
        if self.show_help {
            let mut open = true;
            egui::Window::new("Help")
                .collapsible(false)
                .resizable(false)
                .open(&mut open)
                .show(ctx, |ui| {
                    ui.label("Pattern page: * inserts the original name, # inserts");
                    ui.label("the serial. For example, A_# produces A_<number>.");
                    ui.label("Replace page: replace a string in the file name.");
                    ui.label("Add/Delete page: add or remove text in the name.");
                    ui.label("Regex page: replace a regular expression match.");
                    ui.label("Use $1 or ${name} for capture groups.");
                });
            if !open {
                self.show_help = false;
            }
        }
    }
}

fn pattern_page(ui: &mut egui::Ui, f: &mut PatternForm) {
    ui.label("Naming rule:");
    ui.text_edit_singleline(&mut f.pattern);
    ui.add_space(8.0);
    ui.label("Use * to insert the original file name");
    ui.label("Use # to insert a number or letter at that position");
    ui.add_space(8.0);
    ui.horizontal(|ui| {
        ui.label("Start at");
        ui.add(egui::DragValue::new(&mut f.start));
    });
    ui.horizontal(|ui| {
        ui.label("Step    ");
        ui.add(egui::DragValue::new(&mut f.step));
    });
    ui.horizontal(|ui| {
        ui.label("Digits ");
        ui.add(egui::DragValue::new(&mut f.width).range(1..=16));
    });
    ui.add_space(4.0);
    ui.horizontal(|ui| {
        ui.checkbox(&mut f.pad, "Pad short numbers");
        ui.checkbox(&mut f.letters, "Letter numbering");
        egui::ComboBox::from_id_salt("letter_case")
            .width(90.0)
            .selected_text(match f.letter_case {
                LetterCase::Lower => "Lowercase",
                LetterCase::Upper => "Uppercase",
            })
            .show_ui(ui, |ui| {
                ui.selectable_value(&mut f.letter_case, LetterCase::Lower, "Lowercase");
                ui.selectable_value(&mut f.letter_case, LetterCase::Upper, "Uppercase");
            });
    });
    ui.horizontal(|ui| {
        ui.checkbox(&mut f.change_ext, "Change extension to");
        ui.add(egui::TextEdit::singleline(&mut f.extension).desired_width(80.0));
    });
    ui.add_space(4.0);
    ui.label("File name options");
    egui::ComboBox::from_id_salt("name_case")
        .width(250.0)
        .selected_text(case_label(f.name_case))
        .show_ui(ui, |ui| {
            for c in CASES {
                ui.selectable_value(&mut f.name_case, c, case_label(c));
            }
        });
    ui.add_space(4.0);
    ui.checkbox(&mut f.auto_resolve, "Resolve name conflicts automatically");
}

fn replace_page(ui: &mut egui::Ui, f: &mut ReplaceForm) {
    ui.label("Replace characters in the file name");
    ui.add_space(8.0);
    ui.horizontal(|ui| {
        ui.label("Find         ");
        ui.text_edit_singleline(&mut f.from);
    });
    ui.horizontal(|ui| {
        ui.label("Replace with");
        ui.text_edit_singleline(&mut f.to);
    });
}

fn add_delete_page(ui: &mut egui::Ui, f: &mut AddDeleteForm) {
    ui.horizontal(|ui| {
        ui.label("Add before name ");
        ui.add(egui::TextEdit::singleline(&mut f.prefix).desired_width(200.0));
    });
    ui.horizontal(|ui| {
        ui.label("Add after name  ");
        ui.add(egui::TextEdit::singleline(&mut f.suffix).desired_width(200.0));
    });
    ui.add_space(4.0);
    ui.checkbox(&mut f.ext_add, "Insert at a position");
    ui.horizontal(|ui| {
        ui.label("From character");
        ui.add(egui::DragValue::new(&mut f.insert_pos).range(1..=999));
        ui.label("onwards, insert");
    });
    ui.add(egui::TextEdit::singleline(&mut f.insert_text).desired_width(200.0));
    ui.add_space(4.0);
    ui.horizontal(|ui| {
        ui.label("Delete from name");
        ui.add(egui::TextEdit::singleline(&mut f.delete_text).desired_width(180.0));
    });
    ui.checkbox(&mut f.ext_delete, "Delete a range");
    ui.horizontal(|ui| {
        ui.label("From character");
        ui.add(egui::DragValue::new(&mut f.delete_start).range(1..=999));
        ui.label("delete");
        ui.add(egui::DragValue::new(&mut f.delete_count).range(1..=999));
        ui.label("characters");
    });
}

fn regex_page(ui: &mut egui::Ui, f: &mut RegexForm) {
    ui.label("Find (regular expression)");
    ui.add(egui::TextEdit::singleline(&mut f.from).desired_width(330.0));
    ui.add_space(4.0);
    match Regex::new(&f.from) {
        Ok(_) => {
            ui.label("Use $1 or ${name} in the replacement for capture groups.");
        }
        Err(e) => {
            ui.colored_label(egui::Color32::RED, format!("Invalid expression: {e}"));
        }
    }
    ui.add_space(4.0);
    ui.label("Replace with");
    ui.add(egui::TextEdit::singleline(&mut f.to).desired_width(330.0));
    ui.add_space(4.0);
    ui.label("Example: find (\\w+)_(\\d+) and replace with $2_$1");
}
