// Panel progressive enhancement. Everything here is optional convenience: the
// pages are fully usable with JavaScript disabled or blocked, and nothing is
// sent to the server from this file.
(function () {
  "use strict";

  // --- Copy buttons on .code values ------------------------------------
  // Values that get carried into another interface (a DNS panel, a mail
  // client) sit in a .code-row wrapper next to a Copy button. The text is read
  // from the .code element itself, so it can never drift from what is shown.
  // navigator.clipboard needs a secure context (HTTPS or localhost); over plain
  // HTTP in development it is simply absent, in which case the value stays
  // selectable by hand.
  document.addEventListener("click", function (ev) {
    var button = ev.target.closest("button.copy");
    if (!button) {
      return;
    }
    var row = button.closest(".code-row");
    var code = row && row.querySelector(".code");
    if (!code || !navigator.clipboard) {
      return;
    }
    navigator.clipboard.writeText(code.textContent).then(function () {
      var original = button.textContent;
      button.textContent = "Copied";
      setTimeout(function () {
        button.textContent = original;
      }, 1500);
    }, function () {
      /* Clipboard refused (permissions, insecure context): leave the page be. */
    });
  });

  // --- Confirmation on destructive forms --------------------------------
  // Forms that delete something or invalidate a working credential carry a
  // data-confirm message. The prompt lives here rather than in an inline
  // onsubmit attribute because the panel's Content-Security-Policy allows no
  // inline script. The listener is delegated from the document,
  // so it also covers markup swapped in by HTMX. With JavaScript disabled the
  // form submits without asking — exactly as the inline handler behaved.
  document.addEventListener("submit", function (ev) {
    var form = ev.target.closest("form[data-confirm]");
    if (form && !window.confirm(form.dataset.confirm)) {
      ev.preventDefault();
    }
  });

  // --- Conditional field visibility ------------------------------------
  // Several forms hide a block until a select, checkbox or file input says it
  // applies. One rule table drives them all so the five near-identical helpers
  // do not drift.
  var showWhenRules = [
    {
      match: "select[data-list-mode]",
      target: "[data-addresses]",
      visible: function (el) { return el.value === el.dataset.listMode; }
    },
    {
      match: "select[data-custom-mode]",
      target: "[data-custom-address]",
      visible: function (el) { return el.value === el.dataset.customMode; }
    },
    {
      match: "select[data-global-role]",
      target: "[data-domain-pick]",
      visible: function (el) { return el.value !== el.dataset.globalRole; }
    },
    {
      match: "input[data-encrypt-toggle]",
      target: "[data-encrypt-fields]",
      visible: function (el) { return el.checked; },
      clearWhenHidden: true
    },
    {
      match: "input[data-import-file]",
      target: "[data-import-password-fields]",
      visible: function (el) {
        var name = (el.files && el.files[0] && el.files[0].name || "").toLowerCase();
        return name !== "" && !/\.json$/.test(name);
      },
      clearWhenHidden: true
    }
  ];

  function syncShowWhen(control) {
    var form = control.closest("form");
    if (!form) {
      return;
    }
    for (var i = 0; i < showWhenRules.length; i++) {
      var rule = showWhenRules[i];
      if (!control.matches(rule.match)) {
        continue;
      }
      var target = form.querySelector(rule.target);
      if (!target) {
        return;
      }
      var show = rule.visible(control);
      target.hidden = !show;
      if (!show && rule.clearWhenHidden) {
        target.querySelectorAll("input").forEach(function (input) {
          input.value = "";
        });
      }
      return;
    }
  }

  function initShowWhen(root) {
    showWhenRules.forEach(function (rule) {
      root.querySelectorAll(rule.match).forEach(function (control) {
        syncShowWhen(control);
        control.addEventListener("change", function () {
          syncShowWhen(control);
        });
      });
    });
  }

  document.addEventListener("DOMContentLoaded", function () {
    initShowWhen(document);
  });

  // --- Adaptive monitoring polling ---------------------------------------
  // The four monitoring fragments carry data-poll and hx-trigger="load" for
  // the first fetch only. panel.js schedules the rest: 5 s while the operator
  // is active on the page, 30 s when the tab is visible but idle, and nothing
  // while the tab is hidden. hx-trigger="every Ns [expr]" could express some
  // of that, but the filter is evaluated with `new Function`, which the
  // panel's CSP (default-src 'self', no 'unsafe-eval') would silently break.
  var pollActiveMs = 5000;
  var pollIdleMs = 30000;
  // No pointer/keyboard/scroll input for this long → treat the tab as idle.
  var userIdleMs = 30000;
  var lastActivity = Date.now();
  var pollTimers = Object.create(null);

  ["mousedown", "mousemove", "keydown", "scroll", "touchstart"].forEach(function (evt) {
    document.addEventListener(evt, function () {
      lastActivity = Date.now();
    }, { passive: true });
  });

  function pollDelayMs() {
    return Date.now() - lastActivity < userIdleMs ? pollActiveMs : pollIdleMs;
  }

  function triggerPoll(el) {
    htmx.ajax("GET", el.getAttribute("hx-get"), {
      target: "#" + el.id,
      swap: el.getAttribute("hx-swap") || "outerHTML"
    });
  }

  function schedulePoll(el) {
    if (!el || !el.id || !el.hasAttribute("data-poll")) {
      return;
    }
    if (pollTimers[el.id]) {
      clearTimeout(pollTimers[el.id]);
      delete pollTimers[el.id];
    }
    if (document.hidden) {
      return;
    }
    var id = el.id;
    pollTimers[id] = setTimeout(function () {
      delete pollTimers[id];
      var current = document.getElementById(id);
      if (!current || !current.hasAttribute("data-poll")) {
        return;
      }
      if (document.hidden) {
        schedulePoll(current);
        return;
      }
      triggerPoll(current);
    }, pollDelayMs());
  }

  function onPollElementReady(el) {
    if (!el || !el.hasAttribute("data-poll")) {
      return;
    }
    // Swapped-in markup still carries hx-trigger="load"; strip it so htmx does
    // not issue a duplicate GET on top of the response we just received.
    el.removeAttribute("hx-trigger");
    schedulePoll(el);
  }

  document.body.addEventListener("htmx:afterSwap", function (ev) {
    onPollElementReady(ev.detail.elt);
  });

  document.body.addEventListener("htmx:responseError", function (ev) {
    onPollElementReady(ev.detail.elt);
  });

  document.body.addEventListener("htmx:beforeRequest", function (ev) {
    if (document.hidden && ev.target.hasAttribute && ev.target.hasAttribute("data-poll")) {
      ev.preventDefault();
    }
  });

  document.addEventListener("visibilitychange", function () {
    if (document.hidden) {
      Object.keys(pollTimers).forEach(function (id) {
        clearTimeout(pollTimers[id]);
        delete pollTimers[id];
      });
      return;
    }
    document.querySelectorAll("[data-poll]").forEach(schedulePoll);
  });
})();
