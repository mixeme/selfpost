// The polling of panel.js, run in node against a stand-in for the few parts
// of the DOM and htmx it uses, with a clock the test moves. Run by
// TestPanelJS (paneljs_test.go): node poll_test.js <panel.js>.
// Exits non-zero with what went wrong.
"use strict";
const fs = require("fs");
const vm = require("vm");

const failures = [];
function check(cond, what) {
  if (!cond) failures.push(what);
}

// --- the clock ------------------------------------------------------------
let now = 1000000;
let timers = [];
let nextTimer = 1;
function setTimeout(fn, ms) {
  const id = nextTimer++;
  timers.push({ id, at: now + ms, ms, fn });
  return id;
}
function clearTimeout(id) {
  timers = timers.filter((t) => t.id !== id);
}
function pendingDelays() {
  return timers.map((t) => t.ms);
}
function advance(ms) {
  now += ms;
  const due = timers.filter((t) => t.at <= now);
  timers = timers.filter((t) => t.at > now);
  due.forEach((t) => t.fn());
}
const FakeDate = { now: () => now };

// --- the page ---------------------------------------------------------------
function element(id, attrs) {
  return {
    id,
    attrs: Object.assign({}, attrs),
    hasAttribute(n) { return n in this.attrs; },
    getAttribute(n) { return n in this.attrs ? this.attrs[n] : null; },
    removeAttribute(n) { delete this.attrs[n]; },
  };
}
const listeners = { document: {}, body: {}, window: {} };
function on(where) {
  return (type, fn) => {
    (listeners[where][type] = listeners[where][type] || []).push(fn);
  };
}
let inPage = {};
const document = {
  hidden: false,
  addEventListener: on("document"),
  body: { addEventListener: on("body") },
  getElementById: (id) => inPage[id] || null,
  querySelectorAll: (sel) => (sel === "[data-poll]" ? Object.values(inPage).filter((e) => e.hasAttribute("data-poll")) : []),
};
const window = { addEventListener: on("window"), confirm: () => true };
const requests = [];
const htmx = {
  ajax(verb, path, ctx) {
    requests.push({ verb, path, ctx });
  },
};
// The operator moved the mouse: the page counts as in use for 30 s.
function activity() {
  (listeners.document.mousemove || []).forEach((fn) => fn({}));
}
function fireBody(type, detail) {
  (listeners.body[type] || []).forEach((fn) => fn({ type, detail, target: detail.elt }));
}

vm.runInNewContext(fs.readFileSync(process.argv[2], "utf8"), {
  document, window, htmx, setTimeout, clearTimeout, Date: FakeDate,
  Object, Math, console,
});

// --- the cases --------------------------------------------------------------
const box = element("overview-stats", { "data-poll": "", "hx-get": "/overview/stats", "hx-trigger": "load" });
inPage = { [box.id]: box };

// The first load ended well: the next refresh in 5 s, as the box's source.
fireBody("htmx:afterRequest", { target: box, elt: box, successful: true });
check(pendingDelays().join() === "5000", "after a refresh that worked: waits " + pendingDelays() + ", want 5000");
advance(5000);
check(requests.length === 1, "no refresh was asked for after 5 s");
check(requests[0] && requests[0].ctx.source === box, "the refresh does not name the box as its source");
check(requests[0] && requests[0].ctx.target === "#overview-stats", "the refresh targets " + (requests[0] && requests[0].ctx.target));

// Failures in a row - an error answer, a dropped connection, a timeout -
// each double the wait, up to a minute; a refresh is always scheduled.
const waits = [];
for (let i = 0; i < 5; i++) {
  activity();
  fireBody("htmx:afterRequest", { target: box, elt: box, successful: i === 0 ? false : undefined });
  waits.push(pendingDelays().join());
  advance(Number(pendingDelays()[0] || 0));
}
check(waits.join(" ") === "10000 20000 40000 60000 60000", "the waits after failures in a row: " + waits.join(" ") + ", want 10000 20000 40000 60000 60000");
check(requests.length === 6, "refreshes asked for: " + requests.length + ", want one per wait");

// One that works puts the wait back.
activity();
fireBody("htmx:afterRequest", { target: box, elt: box, successful: true });
check(pendingDelays().join() === "5000", "after failures and a success: waits " + pendingDelays() + ", want 5000");

// A swap replaced the box: the event that reaches the body comes from a
// parent and names the old box as its target; the new one is scheduled.
timers = [];
const fresh = element("overview-stats", { "data-poll": "", "hx-get": "/overview/stats", "hx-trigger": "load" });
inPage = { [fresh.id]: fresh };
fireBody("htmx:afterSwap", { elt: fresh });
check(!fresh.hasAttribute("hx-trigger"), "the swapped-in box keeps hx-trigger");
activity();
fireBody("htmx:afterRequest", { target: box, elt: { id: "main" }, successful: true });
check(pendingDelays().join() === "5000", "after a swap: waits " + pendingDelays() + ", want 5000");
advance(5000);
check(requests[requests.length - 1].ctx.source === fresh, "the refresh after a swap is not the new box's");

// Idle for longer than 30 s: 30 s between refreshes, doubled from there.
timers = [];
now += 31000;
fireBody("htmx:afterRequest", { target: fresh, elt: fresh, successful: true });
check(pendingDelays().join() === "30000", "idle: waits " + pendingDelays() + ", want 30000");
fireBody("htmx:afterRequest", { target: fresh, elt: fresh, successful: false });
check(pendingDelays().join() === "60000", "idle, one failure: waits " + pendingDelays() + ", want 60000");

// A hidden tab schedules nothing.
timers = [];
document.hidden = true;
fireBody("htmx:afterRequest", { target: fresh, elt: fresh, successful: true });
check(timers.length === 0, "a hidden tab scheduled a refresh");
document.hidden = false;

// A request of something that is not a polled box is not its business.
timers = [];
inPage.other = element("other", {});
fireBody("htmx:afterRequest", { target: inPage.other, elt: inPage.other, successful: true });
check(timers.length === 0, "a request of a box that does not poll scheduled a refresh");

if (failures.length) {
  console.log(failures.join("\n"));
  process.exit(1);
}
console.log("ok");
