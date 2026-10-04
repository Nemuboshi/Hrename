// The GUI logic. All rename math happens in Go. This file only moves form
// state in and table rows out.
//
// Compiled by tsc to ../src/main.js (no bundler, no runtime dependencies).
// Plain global script on purpose: ESM imports would point at
// frontend/wailsjs, which is not served by the asset server.

type FormState = {
    tab: string;
    pattern: string;
    start: number;
    step: number;
    width: number;
    pad: boolean;
    letters: boolean;
    letterUpper: boolean;
    changeExt: boolean;
    extension: string;
    autoResolve: boolean;
    nameCase: number;
    from: string;
    to: string;
    prefix: string;
    suffix: string;
    insertAt: boolean;
    insertPos: number;
    insertText: string;
    deleteText: string;
    deleteRange: boolean;
    deleteStart: number;
    deleteCount: number;
};

type RowView = { old: string; preview: string; result: string };
type RunResult = { summary: string; conflict?: string };

interface AppBindings {
    SetForm(f: FormState): Promise<RowView[]>;
    Preview(): Promise<RowView[]>;
    RegexError(expr: string): Promise<string>;
    AddFiles(): Promise<number>;
    AddPaths(paths: string[]): Promise<number>;
    RemoveRow(i: number): Promise<void>;
    RemoveAll(): Promise<void>;
    MoveRow(from: number, to: number): Promise<void>;
    StartRename(): Promise<RunResult>;
    ResolveConflict(policy: string, all: boolean): Promise<RunResult>;
    Quit(): Promise<void>;
}

interface WindowRuntime {
    WindowGetSize(): Promise<{ w: number; h: number }>;
    WindowSetSize(width: number, height: number): void;
    Show(): void;
    OnFileDrop(callback: (x: number, y: number, paths: string[]) => void, useDropTarget: boolean): void;
}

// Global script file: these merge with lib.dom's Window instead of
// shadowing it, so window.go / window.runtime are typed everywhere.
interface Window {
    go?: { main: { App: AppBindings } };
    runtime?: WindowRuntime;
}

const App = (): AppBindings => window.go!.main.App;

let selected = -1;
let rowCount = 0;
let pendingPreview: number | null = null;

// ---------- form <-> Go ----------

function collectForm(): FormState {
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
        insertAt: checked("f-insert-at"),
        insertPos: num("f-insert-pos"),
        insertText: val("f-insert-text"),
        deleteText: val("f-delete-text"),
        deleteRange: checked("f-delete-range"),
        deleteStart: num("f-delete-start"),
        deleteCount: num("f-delete-count"),
    };
}

function val(id: string): string {
    return (document.getElementById(id) as HTMLInputElement).value;
}

function num(id: string): number {
    const n = parseFloat((document.getElementById(id) as HTMLInputElement).value);
    return Number.isFinite(n) ? Math.trunc(n) : 0;
}

function checked(id: string): boolean {
    return (document.getElementById(id) as HTMLInputElement).checked;
}

// refresh sends the form to Go and repaints the table. Input events coalesce
// into one call per animation frame.
function refresh(): void {
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

function renderRows(rows: RowView[]): void {
    rowCount = rows.length;
    const tbody = document.getElementById("file-tbody")!;
    tbody.textContent = "";
    const empty = rows.length === 0;
    document.getElementById("empty-note")!.classList.toggle("hidden", !empty);
    document.getElementById("file-table")!.classList.toggle("hidden", empty);
    if (selected >= rows.length) selected = rows.length - 1;
    rows.forEach((r, i) => {
        const tr = document.createElement("tr");
        if (i === selected) tr.classList.add("selected");
        const tdOld = document.createElement("td");
        tdOld.textContent = r.old;
        tdOld.title = r.old;
        const tdPreview = document.createElement("td");
        tdPreview.textContent = r.preview;
        tdPreview.title = r.preview;
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

function paintSelection(): void {
    const trs = document.getElementById("file-tbody")!.children;
    for (let i = 0; i < trs.length; i++) {
        trs[i].classList.toggle("selected", i === selected);
    }
}

function updateButtons(): void {
    const has = selected >= 0 && selected < rowCount;
    const btn = (id: string) => document.getElementById(id) as HTMLButtonElement;
    btn("btn-remove").disabled = !has;
    btn("btn-remove-all").disabled = rowCount === 0;
    btn("btn-start").disabled = rowCount === 0;
    btn("mv-top").disabled = !(has && selected > 0);
    btn("mv-up").disabled = !(has && selected > 0);
    btn("mv-down").disabled = !(has && selected + 1 < rowCount);
    btn("mv-bottom").disabled = !(has && selected + 1 < rowCount);
}

// ---------- column resizing ----------

// The table is table-layout: fixed with a colgroup, so a column's width is
// the width of its <col>. A handle changes only the column to its left;
// the fixed layout grows the table past the panel when the columns' sum
// exceeds it, and the wrapper turns that into a horizontal scrollbar —
// the point is letting a long file name show in full.
const COL_MIN = 80;
const RESULT_WIDTH = 90;

let colgroup: HTMLTableColElement[] = [];

function headerCellWidth(table: HTMLTableElement, i: number): number {
    return table.tHead!.rows[0].cells[i].offsetWidth;
}

function initColgroup(): void {
    const table = document.getElementById("file-table") as HTMLTableElement;
    const cg = document.createElement("colgroup");
    cg.id = "colgroup";
    const created: HTMLTableColElement[] = [];
    for (let i = 0; i < 3; i++) {
        const c = document.createElement("col");
        if (i === 2) c.style.width = RESULT_WIDTH + "px";
        created.push(c);
        cg.appendChild(c);
    }
    table.prepend(cg);
    colgroup = created;
    // The first two cols have no width yet: the fixed layout then splits
    // the leftover space evenly. Widths appear on first drag.
}

// Before one column grows, pin every column to its measured width. Free
// (auto) columns would otherwise absorb the growth by shrinking to zero,
// and the grown column's name is exactly "the user wants to read it".
function freezeWidths(table: HTMLTableElement): void {
    for (let i = 0; i < 3; i++) {
        colgroup[i].style.width = headerCellWidth(table, i) + "px";
    }
}

function initColumnResize(): void {
    const table = document.getElementById("file-table") as HTMLTableElement;
    table.querySelectorAll<HTMLElement>(".col-split").forEach((handle) => {
        const left = Number(handle.dataset.col); // column index left of the handle
        handle.addEventListener("pointerdown", (ev) => {
            ev.preventDefault();
            freezeWidths(table);
            const startX = ev.clientX;
            const w0 = headerCellWidth(table, left);
            document.body.classList.add("col-resizing");
            handle.classList.add("active");
            handle.setPointerCapture(ev.pointerId);

            const move = (e: PointerEvent) => {
                const w = Math.max(COL_MIN, w0 + (e.clientX - startX));
                colgroup[left].style.width = w + "px";
            };
            const up = () => {
                handle.removeEventListener("pointermove", move);
                handle.removeEventListener("pointerup", up);
                handle.removeEventListener("pointercancel", up);
                document.body.classList.remove("col-resizing");
                handle.classList.remove("active");
            };
            handle.addEventListener("pointermove", move);
            handle.addEventListener("pointerup", up);
            handle.addEventListener("pointercancel", up);
        });
    });
}

// ---------- tabs ----------

let activeTab = "whole";

function showTab(tab: string): void {
    activeTab = tab;
    document.querySelectorAll<HTMLButtonElement>(".tab").forEach((b) => {
        b.classList.toggle("active", b.dataset.tab === tab);
    });
    document.querySelectorAll(".page").forEach((p) => {
        p.classList.toggle("active", p.id === "page-" + tab);
    });
    refresh();
}

// ---------- dependent controls ----------

function syncDependent(): void {
    (document.getElementById("f-letter-case") as HTMLSelectElement).disabled = !checked("f-letters");
    (document.getElementById("f-extension") as HTMLInputElement).disabled = !checked("f-change-ext");
    document.getElementById("insert-block")!.style.opacity = checked("f-insert-at") ? "1" : "0.5";
    document.getElementById("delete-block")!.style.opacity = checked("f-delete-range") ? "1" : "0.5";
}

// ---------- regex validation ----------

async function validateRegex(): Promise<void> {
    const note = document.getElementById("regex-note")!;
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

async function addFiles(): Promise<void> {
    try {
        const added = await App().AddFiles();
        if (added > 0) {
            renderRows(await App().Preview());
        }
    } catch (e) {
        console.error(e);
    }
}

async function moveRow(to: number): Promise<void> {
    if (selected < 0) return;
    try {
        await App().MoveRow(selected, to);
        selected = to;
        renderRows(await App().Preview());
    } catch (e) {
        console.error(e);
    }
}

async function removeRow(): Promise<void> {
    if (selected < 0) return;
    try {
        await App().RemoveRow(selected);
        selected = Math.min(selected, rowCount - 2);
        renderRows(await App().Preview());
    } catch (e) {
        console.error(e);
    }
}

async function removeAll(): Promise<void> {
    try {
        await App().RemoveAll();
        selected = -1;
        renderRows(await App().Preview());
    } catch (e) {
        console.error(e);
    }
}

// ---------- rename run ----------

async function startRename(): Promise<void> {
    try {
        const res = await App().StartRename();
        renderRows(await App().Preview());
        showRunResult(res);
    } catch (e) {
        console.error(e);
    }
}

function showRunResult(res: RunResult): void {
    if (res.conflict) {
        document.getElementById("conflict-text")!.textContent =
            "The file name \"" + res.conflict + "\" already exists.";
        openModal("modal-conflict");
    } else if (res.summary) {
        document.getElementById("summary-text")!.textContent = res.summary;
        openModal("modal-summary");
    }
}

async function resolveConflict(policy: string): Promise<void> {
    const all = checked("conflict-all");
    closeModal("modal-conflict");
    try {
        const res = await App().ResolveConflict(policy, all);
        renderRows(await App().Preview());
        showRunResult(res);
    } catch (e) {
        console.error(e);
    }
}

// ---------- modals ----------

function openModal(id: string): void {
    document.getElementById(id)!.classList.remove("hidden");
}

function closeModal(id: string): void {
    document.getElementById(id)!.classList.add("hidden");
}

// ---------- window sizing ----------

// The window is not resizable, so its size must come from the content.
// Font metrics and window frames differ between platforms, so the Go side
// cannot know the right size. The app starts hidden; we measure the
// intrinsic content, resize the outer window by the amount the viewport
// misses the target, then show. The frame drops out of the delta because
// it is the same before and after.
function measureContent(): { w: number; h: number } {
    const layout = document.getElementById("layout")!;
    const cs = getComputedStyle(layout);
    const gap = parseFloat(cs.columnGap) || parseFloat(cs.gap) || 0;
    const w =
        document.getElementById("rules")!.offsetWidth +
        document.getElementById("list-panel")!.offsetWidth +
        gap + parseFloat(cs.paddingLeft) + parseFloat(cs.paddingRight);
    return { w: Math.ceil(w), h: Math.ceil(document.body.offsetHeight) };
}

async function fitWindow(round: number): Promise<void> {
    const want = measureContent();
    const dw = want.w - window.innerWidth;
    const dh = want.h - window.innerHeight;
    if (dw === 0 && dh === 0) return;
    const outer = await window.runtime!.WindowGetSize();
    window.runtime!.WindowSetSize(outer.w + dw, outer.h + dh);
    // The resize lands asynchronously: the call only posts a message to
    // the Go UI thread, which resizes the window and relayouts the
    // webview, and Windows rounds physical pixels to logical ones with
    // integer division, so one pass can end up a pixel off. The 80ms wait
    // is a heuristic, not a measured constant: about two 60Hz frames plus
    // the cross-thread post, i.e. comfortably past the usual completion
    // time, while the worst case of three passes stays imperceptible.
    if (round < 2) setTimeout(() => void fitWindow(round + 1), 80);
}

async function fitAndReveal(): Promise<void> {
    try {
        await fitWindow(0);
    } finally {
        window.runtime!.Show();
    }
}

// ---------- wiring ----------

window.addEventListener("DOMContentLoaded", () => {
    initColgroup();
    initColumnResize();

    document.querySelectorAll<HTMLButtonElement>(".tab").forEach((b) => {
        b.addEventListener("click", () => showTab(b.dataset.tab!));
    });

    document.querySelectorAll<HTMLInputElement | HTMLSelectElement>("#rules input, #rules select").forEach((el) => {
        const handler = () => {
            syncDependent();
            if (el.id === "f-from-regex") validateRegex();
            refresh();
        };
        el.addEventListener("input", handler);
        el.addEventListener("change", handler);
    });

    document.getElementById("btn-add")!.addEventListener("click", addFiles);
    document.getElementById("btn-remove")!.addEventListener("click", removeRow);
    document.getElementById("btn-remove-all")!.addEventListener("click", removeAll);
    document.getElementById("mv-top")!.addEventListener("click", () => moveRow(0));
    document.getElementById("mv-up")!.addEventListener("click", () => moveRow(selected - 1));
    document.getElementById("mv-down")!.addEventListener("click", () => moveRow(selected + 1));
    document.getElementById("mv-bottom")!.addEventListener("click", () => moveRow(rowCount - 1));

    document.getElementById("btn-start")!.addEventListener("click", startRename);
    document.getElementById("btn-cancel")!.addEventListener("click", () => void App().Quit());
    document.getElementById("btn-help")!.addEventListener("click", () => openModal("modal-help"));

    document.querySelectorAll<HTMLButtonElement>("[data-conflict]").forEach((b) => {
        b.addEventListener("click", () => resolveConflict(b.dataset.conflict!));
    });
    document.getElementById("summary-ok")!.addEventListener("click", () => closeModal("modal-summary"));
    document.getElementById("help-ok")!.addEventListener("click", () => closeModal("modal-help"));

    // Dropped files. The second argument false means the whole window
    // accepts drops, no per-element --wails-drop-target style needed.
    // This JS call also stops WebView2 from treating the drop as a download.
    if (window.runtime) {
        window.runtime.OnFileDrop(async (_x, _y, paths) => {
            if (!paths || paths.length === 0) return;
            const added = await App().AddPaths(paths);
            if (added > 0) {
                renderRows(await App().Preview());
            }
        }, false);

        // Not in requestAnimationFrame: animation frames are throttled
        // while the window is hidden, and the fit must run before the
        // reveal.
        void fitAndReveal();
    }

    syncDependent();
    refresh();
});
