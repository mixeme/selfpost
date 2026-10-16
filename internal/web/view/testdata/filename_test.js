// The name of the file chosen in a Bulma .file field is written into its
// .file-name (the import of a domain on Server › Backup): on change, and at
// load for a page the browser restored with a file still chosen. Run by
// TestPanelJS (paneljs_test.go): node filename_test.js <panel.js>. Exits
// non-zero with what went wrong.
"use strict";
const fs = require("fs");
const vm = require("vm");

const failures = [];
function check(cond, what) {
  if (!cond) failures.push(what);
}

const listeners = {};
const label = { textContent: "No file chosen" };
const box = { querySelector: (sel) => (sel === ".file-name" ? label : null) };
const input = {
  files: [],
  matches: (sel) => sel === "input.file-input",
  closest: (sel) => (sel === ".file" ? box : null),
};
const document = {
  hidden: false,
  addEventListener: (type, fn) => (listeners[type] = listeners[type] || []).push(fn),
  body: { addEventListener() {} },
  getElementById: () => null,
  querySelectorAll: (sel) => (sel === "input.file-input" ? [input] : []),
};
vm.runInNewContext(fs.readFileSync(process.argv[2], "utf8"), {
  document, window: { addEventListener() {} }, htmx: { config: {}, ajax() {} }, setTimeout, clearTimeout, Date, Object, Math, console,
});
const fire = (type, ev) => (listeners[type] || []).forEach((fn) => fn(ev));

input.files = [{ name: "example.org.spde" }];
fire("change", { target: input });
check(label.textContent === "example.org.spde", `after choosing a file the name reads ${JSON.stringify(label.textContent)}`);

input.files = [];
fire("change", { target: input });
check(label.textContent === "No file chosen", `after clearing the choice the name reads ${JSON.stringify(label.textContent)}`);

input.files = [{ name: "restored.json" }];
fire("DOMContentLoaded", {});
check(label.textContent === "restored.json", `a restored page shows ${JSON.stringify(label.textContent)}`);

if (failures.length) {
  console.error(failures.join("\n"));
  process.exit(1);
}
