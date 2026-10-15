// The question panel.js asks before a button that leaves the page - a rate
// limit's recalculation - when a field holds an edit not saved yet (audit
// 2026-10-12, L19). Run by TestPanelJS (paneljs_test.go): node
// unsaved_test.js <panel.js>. Exits non-zero with what went wrong.
"use strict";
const fs = require("fs");
const vm = require("vm");

const failures = [];
function check(cond, what) {
  if (!cond) failures.push(what);
}

const listeners = {};
let fields = [];
const document = {
  hidden: false,
  addEventListener: (type, fn) => (listeners[type] = listeners[type] || []).push(fn),
  body: { addEventListener() {} },
  getElementById: () => null,
  querySelectorAll: (sel) => (sel === "input, select, textarea" ? fields : []),
};
let asked = [];
let answer = false;
const window = {
  addEventListener() {},
  confirm(q) {
    asked.push(q);
    return answer;
  },
};
vm.runInNewContext(fs.readFileSync(process.argv[2], "utf8"), {
  document, window, htmx: { config: {}, ajax() {} }, setTimeout, clearTimeout, Date, Object, Math, console,
});

function input(type, value, defaultValue) {
  return { tagName: "INPUT", type, value, defaultValue, disabled: false };
}
function box(checked, defaultChecked) {
  return { tagName: "INPUT", type: "checkbox", checked, defaultChecked, disabled: false };
}
function select(selectedIndex, defaults, opts) {
  return Object.assign({
    tagName: "SELECT", type: "select-one", selectedIndex, disabled: false, multiple: false, size: 0,
    options: defaults.map((d) => ({ defaultSelected: d })),
  }, opts || {});
}

// submit fires the document's submit listeners for a form with no
// data-confirm, sent by a button with the given data-unsaved, and says
// whether the submission went on.
function submit(unsaved) {
  let prevented = false;
  const ev = {
    target: { closest: () => null },
    submitter: { dataset: unsaved === undefined ? {} : { unsaved } },
    preventDefault() { prevented = true; },
  };
  (listeners.submit || []).forEach((fn) => fn(ev));
  return !prevented;
}

const Q = "Recalculating leaves this page. Recalculate anyway?";
const drawn = () => [
  input("text", "100", "100"),
  input("hidden", "x", "y"), // never an edit
  box(true, true),
  select(0, [false, false, false]), // no option says selected: the first
  select(2, [false, false, true]),
];

function scenario(name, edit, wantAsked) {
  fields = drawn();
  edit(fields);
  asked = [];
  answer = false;
  const went = submit(Q);
  check(asked.length === (wantAsked ? 1 : 0), name + ": asked " + asked.length + " times, want " + (wantAsked ? 1 : 0));
  check(went === !wantAsked, name + ": the submission " + (went ? "went on" : "was stopped"));
}

scenario("nothing changed", () => {}, false);
scenario("a number typed", (f) => (f[0].value = "150"), true);
scenario("a box unticked", (f) => (f[2].checked = false), true);
scenario("a select moved off its first option", (f) => (f[3].selectedIndex = 1), true);
scenario("a select moved off its selected option", (f) => (f[4].selectedIndex = 0), true);
scenario("a disabled field differs", (f) => { f[0].value = "150"; f[0].disabled = true; }, false);
// A password manager filling a password field, a file chosen: not edits.
scenario("a password filled in", (f) => f.push(input("password", "secret", "")), false);
scenario("a file chosen", (f) => f.push(input("file", "x.json", "")), false);
// A list that takes several: any option off what was drawn.
scenario("a multiple select changed", (f) => f.push(Object.assign(select(-1, [false, false]), {
  multiple: true, options: [{ selected: true, defaultSelected: false }, { selected: false, defaultSelected: false }],
})), true);
scenario("a multiple select as drawn", (f) => f.push(Object.assign(select(-1, [false, false]), {
  multiple: true, options: [{ selected: true, defaultSelected: true }, { selected: false, defaultSelected: false }],
})), false);

// Answered yes: the submission goes on.
fields = drawn();
fields[0].value = "150";
asked = [];
answer = true;
check(submit(Q) === true && asked.length === 1, "answered yes, the recalculation did not go on");

// A button without the question - Save - never asks.
asked = [];
answer = false;
check(submit(undefined) === true && asked.length === 0, "a plain submit asked or was stopped");

if (failures.length) {
  console.log(failures.join("\n"));
  process.exit(1);
}
console.log("ok");
