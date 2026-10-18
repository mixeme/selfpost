// The user form (Server › Users): with "All outbound domains" ticked the
// single outbound domains are disabled, as the mockup user.html draws them, and
// enabled again when it is unticked; the inbound list follows its own All the
// same way, and neither touches the other. Run by TestPanelJS
// (paneljs_test.go): node alldomains_test.js <panel.js>. Exits non-zero with
// what went wrong.
"use strict";
const fs = require("fs");
const vm = require("vm");

const failures = [];
function check(cond, what) {
  if (!cond) failures.push(what);
}

const listeners = {};
const box = (attr, checked) => ({ attr, checked, disabled: false, matches: (sel) => sel === "input[" + attr + "]", closest: () => form });
const allOut = box("data-all-out", true);
const allIn = box("data-all-in", false);
const singlesOut = [box("data-single-out", true), box("data-single-out", false)];
const singlesIn = [box("data-single-in", true)];
const form = {
  querySelectorAll: (sel) =>
    sel === "input[data-single-out]" ? singlesOut : sel === "input[data-single-in]" ? singlesIn : [],
};
const document = {
  hidden: false,
  addEventListener: (type, fn) => (listeners[type] = listeners[type] || []).push(fn),
  body: { addEventListener() {} },
  getElementById: () => null,
  querySelectorAll: (sel) =>
    sel === "input[data-all-out]" ? [allOut] : sel === "input[data-all-in]" ? [allIn] : [],
};
vm.runInNewContext(fs.readFileSync(process.argv[2], "utf8"), {
  document, window: { addEventListener() {} }, htmx: { config: {}, ajax() {} }, setTimeout, clearTimeout, Date, Object, Math, console,
});
const fire = (type, ev) => (listeners[type] || []).forEach((fn) => fn(ev));

fire("DOMContentLoaded", {});
check(singlesOut.every((b) => b.disabled), "at load, with All outbound ticked, the single outbound domains are not disabled");
check(singlesIn.every((b) => !b.disabled), "at load, with All inbound unticked, a single inbound domain is disabled");

allOut.checked = false;
fire("change", { target: allOut });
check(singlesOut.every((b) => !b.disabled), "unticking All outbound leaves the single outbound domains disabled");
check(singlesOut[0].checked && !singlesOut[1].checked, "unticking All outbound changed which single domains are ticked");

allIn.checked = true;
fire("change", { target: allIn });
check(singlesIn.every((b) => b.disabled), "ticking All inbound does not disable the single inbound domains");
check(singlesOut.every((b) => !b.disabled), "ticking All inbound disabled the outbound list");

if (failures.length) {
  console.error(failures.join("\n"));
  process.exit(1);
}
