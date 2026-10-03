// The GUI logic. All rename math happens in Go. This file only moves form
// state in and table rows out.

const App = () => window.go.main.App;

let selected = -1;
let rowCount = 0;
let pendingPreview = null;

// ---------- form <-> Go ----------

function collectForm() {
    return {
        tab: activeTab,
        pattern: val("f-pattern"),
        start: num("f-start"),
        step: num("f-step"),
        width: Math.max(1, num("f-width")),
        pad: checked("f-pad"),
        letters: checked("f-letters"),
        letterUpper: val("f-letter-case") === "1",
        changeExt: checked("f-change-ext"),
        extension: val("f-extension"),
        nameCase: parseInt(val("f-name-case"), 10) || 0,
        autoResolve: checked("f-auto-resolve"),
        from: activeTab === "regex" ? val("f-from-regex") : val("f-from-rep"),
        to: activeTab === "regex" ? val("f-to-regex") : val("f-to-rep"),
        prefix: val("f-prefix"),
        suffix: val("f-suffix"),
        extAdd: checked("f-ext-add"),
        insertPos: num("f-insert-pos"),
        insertText: val("f-insert-text"),
        deleteText: val("f-delete-text"),
        extDelete: checked("f-ext-delete"),
        deleteStart: num("f-delete-start"),
        deleteCount: num("f-delete-count"),
    };
}

function val(id) {
    return document.getElementById(id).value;
}

function num(id) {
    const n = parseFloat(document.getElementById(id).value);
    return Number.isFinite(n) ? Math.trunc(n) : 0;
}

function checked(id) {
    return document.getElementById(id).checked;
}

// refresh sends the form to Go and repaints the table. Input events coalesce
// into one call per animation frame.
function refresh() {
    if (pendingPreview) return;
    pendingPreview = requestAnimationFrame(async () => {
        pendingPreview = null;
        try {
            const rows = await App().SetForm(collectForm());
            renderRows(rows);
        } catch (e) {
            console.error(e);
        }
    });
}

// ---------- table ----------

function renderRows(rows) {
    rowCount = rows.length;
    const tbody = document.getElementById("file-tbody");
    tbody.textContent = "";
    const empty = rows.length === 0;
    document.getElementById("empty-note").classList.toggle("hidden", !empty);
    document.getElementById("file-table").classList.toggle("hidden", empty);
    if (selected >= rows.length) selected = rows.length - 1;
    rows.forEach((r, i) => {
        const tr = document.createElement("tr");
        if (i === selected) tr.classList.add("selected");
        const tdOld = document.createElement("td");
        tdOld.textContent = r.old;
        tdOld.title = r.old;
        const tdPreview = document.createElement("td");
        tdPreview.textContent = r.preview;
        const tdResult = document.createElement("td");
        tdResult.className = "col-result";
        tdResult.textContent = r.result;
        tr.append(tdOld, tdPreview, tdResult);
        tr.addEventListener("mousedown", () => {
            selected = i;
            paintSelection();
            updateButtons();
        });
        tbody.appendChild(tr);
    });
    updateButtons();
}

function paintSelection() {
    const trs = document.getElementById("file-tbody").children;
    for (let i = 0; i < trs.length; i++) {
        trs[i].classList.toggle("selected", i === selected);
    }
}

function updateButtons() {
    const has = selected >= 0 && selected < rowCount;
    document.getElementById("btn-remove").disabled = !has;
    document.getElementById("btn-remove-all").disabled = rowCount === 0;
    document.getElementById("btn-start").disabled = rowCount === 0;
    document.getElementById("mv-top").disabled = !(has && selected > 0);
    document.getElementById("mv-up").disabled = !(has && selected > 0);
    document.getElementById("mv-down").disabled = !(has && selected + 1 < rowCount);
    document.getElementById("mv-bottom").disabled = !(has && selected + 1 < rowCount);
}

// ---------- tabs ----------

let activeTab = "whole";

function showTab(tab) {
    activeTab = tab;
    document.querySelectorAll(".tab").forEach((b) => {
        b.classList.toggle("active", b.dataset.tab === tab);
    });
    document.querySelectorAll(".page").forEach((p) => {
        p.classList.toggle("active", p.id === "page-" + tab);
    });
    refresh();
}

// ---------- dependent controls ----------

function syncDependent() {
    document.getElementById("f-letter-case").disabled = !checked("f-letters");
    document.getElementById("f-extension").disabled = !checked("f-change-ext");
    document.getElementById("insert-block").style.opacity = checked("f-ext-add") ? "1" : "0.5";
    document.getElementById("delete-block").style.opacity = checked("f-ext-delete") ? "1" : "0.5";
}

// ---------- regex validation ----------

async function validateRegex() {
    const note = document.getElementById("regex-note");
    const expr = val("f-from-regex");
    try {
        const err = await App().RegexError(expr);
        if (err) {
            note.textContent = "Invalid expression: " + err;
            note.classList.add("error");
        } else {
            note.textContent = "Use $1 or ${name} in the replacement for capture groups.";
            note.classList.remove("error");
        }
    } catch (e) {
        console.error(e);
    }
}

// ---------- list actions ----------

async function addFiles() {
    try {
        const added = await App().AddFiles();
        if (added > 0) {
            const rows = await App().Preview();
            renderRows(rows);
        }
    } catch (e) {
        console.error(e);
    }
}

async function moveRow(to) {
    if (selected < 0) return;
    try {
        await App().MoveRow(selected, to);
        selected = to;
        const rows = await App().Preview();
        renderRows(rows);
        paintSelection();
    } catch (e) {
        console.error(e);
    }
}

async function removeRow() {
    if (selected < 0) return;
    try {
        await App().RemoveRow(selected);
        selected = Math.min(selected, rowCount - 2);
        const rows = await App().Preview();
        renderRows(rows);
    } catch (e) {
        console.error(e);
    }
}

async function removeAll() {
    try {
        await App().RemoveAll();
        selected = -1;
        const rows = await App().Preview();
        renderRows(rows);
    } catch (e) {
        console.error(e);
    }
}

// ---------- rename run ----------

async function startRename() {
    try {
        const res = await App().StartRename();
        const rows = await App().Preview();
        renderRows(rows);
        showRunResult(res);
    } catch (e) {
        console.error(e);
    }
}

function showRunResult(res) {
    if (res.conflict) {
        document.getElementById("conflict-text").textContent =
            "The file name \"" + res.conflict.name + "\" already exists.";
        openModal("modal-conflict");
    } else if (res.summary) {
        document.getElementById("summary-text").textContent = res.summary;
        openModal("modal-summary");
    }
}

async function resolveConflict(policy) {
    const all = checked("conflict-all");
    closeModal("modal-conflict");
    try {
        const res = await App().ResolveConflict(policy, all);
        const rows = await App().Preview();
        renderRows(rows);
        showRunResult(res);
    } catch (e) {
        console.error(e);
    }
}

// ---------- modals ----------

function openModal(id) {
    document.getElementById(id).classList.remove("hidden");
}

function closeModal(id) {
    document.getElementById(id).classList.add("hidden");
}

// ---------- wiring ----------

window.addEventListener("DOMContentLoaded", () => {
    document.querySelectorAll(".tab").forEach((b) => {
        b.addEventListener("click", () => showTab(b.dataset.tab));
    });

    document.querySelectorAll("#rules input, #rules select").forEach((el) => {
        const handler = () => {
            syncDependent();
            if (el.id === "f-from-regex") validateRegex();
            refresh();
        };
        el.addEventListener("input", handler);
        el.addEventListener("change", handler);
    });

    document.getElementById("btn-add").addEventListener("click", addFiles);
    document.getElementById("btn-remove").addEventListener("click", removeRow);
    document.getElementById("btn-remove-all").addEventListener("click", removeAll);
    document.getElementById("mv-top").addEventListener("click", () => moveRow(0));
    document.getElementById("mv-up").addEventListener("click", () => moveRow(selected - 1));
    document.getElementById("mv-down").addEventListener("click", () => moveRow(selected + 1));
    document.getElementById("mv-bottom").addEventListener("click", () => moveRow(rowCount - 1));

    document.getElementById("btn-start").addEventListener("click", startRename);
    document.getElementById("btn-cancel").addEventListener("click", () => App().Quit());
    document.getElementById("btn-help").addEventListener("click", () => openModal("modal-help"));

    document.querySelectorAll("[data-conflict]").forEach((b) => {
        b.addEventListener("click", () => resolveConflict(b.dataset.conflict));
    });
    document.getElementById("summary-ok").addEventListener("click", () => closeModal("modal-summary"));
    document.getElementById("help-ok").addEventListener("click", () => closeModal("modal-help"));

    if (window.runtime) {
        window.runtime.EventsOn("files-changed", async () => {
            const rows = await App().Preview();
            renderRows(rows);
        });
    }

    syncDependent();
    refresh();
});
