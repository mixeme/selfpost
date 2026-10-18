// Panel progressive enhancement. Everything here is optional convenience: the
// pages are fully usable with JavaScript disabled or blocked, and nothing is
// sent to the server from this file.
(function () {
  "use strict";

  // --- Copy buttons ---------------------------------------------------
  // copy_field joins a read-only input or textarea to a Copy button marked
  // data-copy; the two sit together in one .has-addons field, and the value is
  // read from the field itself, so it can never drift from what is shown. Where
  // the clipboard API is absent (plain HTTP outside localhost) the value is
  // selected instead, ready for the operator's own Ctrl+C. The button's icon
  // stays; only its word changes for a moment. W3C Clipboard API, § Navigator
  // interface: navigator.clipboard is [SecureContext].
  document.addEventListener("click", function (ev) {
    var button = ev.target.closest("button[data-copy]");
    if (!button) {
      return;
    }
    var group = button.closest(".has-addons");
    var field = group && group.querySelector("input, textarea");
    if (!field) {
      return;
    }
    if (!navigator.clipboard) {
      field.focus();
      field.select();
      return;
    }
    navigator.clipboard.writeText(field.value).then(function () {
      var label = button.lastChild;
      if (!label || label.nodeType !== Node.TEXT_NODE) {
        return;
      }
      var original = label.nodeValue;
      label.nodeValue = "Copied";
      setTimeout(function () {
        label.nodeValue = original;
      }, 1500);
    }, function () {
      field.focus();
      field.select();
    });
  });

  // --- Fill buttons -----------------------------------------------------
  // A type="button" with data-fill="<id of an input>" and data-fill-value="<text>"
  // puts the text in that input and tells the page it changed. It only fills:
  // saving stays the form's own button. Without scripts the button does nothing
  // and the field can be typed in.
  document.addEventListener("click", function (ev) {
    var button = ev.target.closest("button[data-fill]");
    if (!button) {
      return;
    }
    var input = document.getElementById(button.dataset.fill);
    if (!input) {
      return;
    }
    input.value = button.dataset.fillValue || "";
    input.dispatchEvent(new Event("input", { bubbles: true }));
    input.dispatchEvent(new Event("change", { bubbles: true }));
    input.focus();
  });

  // --- Secrets shown once ------------------------------------------------
  // A password shown once is in a field marked data-secret. Cache-Control:
  // no-store keeps the page out of HTTP caches, but a browser may still keep
  // the page in its back/forward cache and show it again on Back (RFC 9111
  // § 6: a history mechanism "might honor" no-store). Emptying the field when
  // the page is left means a restored page shows nothing to copy. HTML Living
  // Standard, § 7.5.9 "Unloading documents", "unload a Document", step 10.2:
  // pagehide is fired whether or not the document is kept for later history
  // traversal (its salvageable state, the event's persisted).
  window.addEventListener("pagehide", function () {
    var secrets = document.querySelectorAll("[data-secret]");
    // defaultValue too: it is the input's value attribute and the
    // textarea's text, which the kept page still holds after .value is
    // emptied (HTML, "The input element": defaultValue reflects the value
    // content attribute; "The textarea element": its setter replaces the
    // child text).
    for (var i = 0; i < secrets.length; i++) {
      secrets[i].value = "";
      secrets[i].defaultValue = "";
    }
  });

  // --- Confirmation on destructive forms --------------------------------
  // Forms that delete something or invalidate a working credential carry a
  // data-confirm message. The prompt lives here rather than in an inline
  // onsubmit attribute because the panel's Content-Security-Policy allows no
  // inline script. The listener is delegated from the document,
  // so it also covers markup swapped in by HTMX. With JavaScript disabled the
  // form submits without asking — exactly as the inline handler behaved.
  // HTML Living Standard, form-control-infrastructure.html#form-submission-algorithm:
  // submit is fired with bubbles and cancelable true, and a cancelled one
  // ends the submission.
  document.addEventListener("submit", function (ev) {
    var form = ev.target.closest("form[data-confirm]");
    if (form && !window.confirm(form.dataset.confirm)) {
      ev.preventDefault();
      return;
    }
    // A button that does something else than save - recalculate a rate
    // limit - leaves the page, and what was typed on it and not saved goes
    // with it. Such a button carries data-unsaved, the question to ask when
    // a field of the page differs from what the page was drawn with (audit
    // 2026-10-12, L19). HTML Living Standard, SubmitEvent: submitter is the
    // button that submitted the form.
    var button = ev.submitter;
    if (button && button.dataset && button.dataset.unsaved && hasUnsavedEdit() &&
        !window.confirm(button.dataset.unsaved)) {
      ev.preventDefault();
    }
  });

  // hasUnsavedEdit tells whether a field of the page holds a value the page
  // was not drawn with: defaultValue, defaultChecked and defaultSelected are
  // what the markup said (HTML Living Standard, the input, select and
  // textarea elements). A select none of whose options says selected was
  // drawn with its first option, or none when it is a list (size or multiple).
  // Password and file fields are not edits of the page: a password manager
  // fills the first, nothing draws the second (review of stage 4).
  function hasUnsavedEdit() {
    var fields = document.querySelectorAll("input, select, textarea");
    for (var i = 0; i < fields.length; i++) {
      var f = fields[i];
      if (f.disabled || f.type === "hidden" || f.type === "submit" || f.type === "button" ||
          f.type === "reset" || f.type === "password" || f.type === "file") {
        continue;
      }
      if (f.type === "checkbox" || f.type === "radio") {
        if (f.checked !== f.defaultChecked) {
          return true;
        }
      } else if (f.tagName === "SELECT" && f.multiple) {
        for (var m = 0; m < f.options.length; m++) {
          if (f.options[m].selected !== f.options[m].defaultSelected) {
            return true;
          }
        }
      } else if (f.tagName === "SELECT") {
        var drawn = -1;
        for (var j = 0; j < f.options.length; j++) {
          if (f.options[j].defaultSelected) {
            drawn = j;
            break;
          }
        }
        if (drawn < 0 && !f.multiple && f.size <= 1 && f.options.length > 0) {
          drawn = 0;
        }
        if (f.selectedIndex !== drawn) {
          return true;
        }
      } else if (f.value !== f.defaultValue) {
        return true;
      }
    }
    return false;
  }

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
      match: "select[data-ratelimit-mode]",
      target: "[data-manual-fields]",
      visible: function (el) { return el.value === "manual"; }
    },
    {
      match: "select[data-ratelimit-mode]",
      target: "[data-auto-fields]",
      visible: function (el) { return el.value === "auto"; }
    },
    {
      match: "select[data-global-role]",
      target: "[data-domain-pick]",
      visible: function (el) { return el.value !== el.dataset.globalRole; }
    },
    // The user form: with "All outbound domains" (or inbound) ticked the
    // single domains of that list are ignored, and are shown disabled as the
    // mockup user.html draws them. A disabled box is not submitted (HTML
    // Living Standard, form-control-infrastructure.html#constructing-entry-list),
    // which changes nothing: the server ignores them with All. Without this
    // script they stay clickable, and the help line says they are ignored.
    {
      match: "input[data-all-out]",
      target: "input[data-single-out]",
      visible: function (el) { return !el.checked; },
      disable: true
    },
    {
      match: "input[data-all-in]",
      target: "input[data-single-in]",
      visible: function (el) { return !el.checked; },
      disable: true
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
    // A control can drive more than one block (the rate-limit select shows the
    // manual fields or the auto ones), so every rule that matches is applied.
    for (var i = 0; i < showWhenRules.length; i++) {
      var rule = showWhenRules[i];
      if (!control.matches(rule.match)) {
        continue;
      }
      // A block can be several elements (the user form's two lists of domains).
      var show = rule.visible(control);
      form.querySelectorAll(rule.target).forEach(function (target) {
        if (rule.disable) {
          target.disabled = !show;
          return;
        }
        // hidden alone does not hide an element whose own stylesheet gives it a
        // display (Bulma's .box), so Bulma's is-hidden helper goes with it.
        // HTML Living Standard, rendering.html#hidden-elements: [hidden] is
        // display: none in the user agent's sheet, which an author rule such
        // as Bulma 1.0.4's .box { display: block } overrides; .is-hidden is
        // display: none !important.
        target.hidden = !show;
        target.classList.toggle("is-hidden", !show);
        if (!show && rule.clearWhenHidden) {
          target.querySelectorAll("input").forEach(function (input) {
            input.value = "";
          });
        }
      });
    }
  }

  function initShowWhen(root) {
    showWhenRules.forEach(function (rule) {
      root.querySelectorAll(rule.match).forEach(syncShowWhen);
    });
  }

  // A file field drawn as Bulma's .file (the import of a domain, mockup
  // backup.html): the input lies transparent over "Choose a file…", and the
  // name of the file chosen is written into .file-name, which says "No file
  // chosen" until then. Without this script the field still works; only the
  // name is not shown.
  function syncFileName(input) {
    var box = input.closest(".file");
    var name = box && box.querySelector(".file-name");
    if (name) {
      name.textContent = input.files && input.files[0] ? input.files[0].name : "No file chosen";
    }
  }

  // One listener for the document rather than one per control: a form that
  // arrives in an htmx swap is covered without being wired up again.
  document.addEventListener("change", function (ev) {
    if (ev.target && ev.target.matches) {
      syncShowWhen(ev.target);
      if (ev.target.matches("input.file-input")) {
        syncFileName(ev.target);
      }
    }
  });

  // A page the browser restores from its history may keep the file chosen.
  document.addEventListener("DOMContentLoaded", function () {
    initShowWhen(document);
    document.querySelectorAll("input.file-input").forEach(syncFileName);
  });

  // htmx:load fires for each top-level element htmx puts into the page, so
  // swapped-in markup starts with its blocks shown or hidden the way a full
  // page does. htmx 2.0.4, src/htmx.js insertNodesBefore() queues
  // makeAjaxLoadTask(child) for each element of the swapped content, which
  // triggers htmx:load on it (detail.elt); initShowWhen looks inside it.
  document.addEventListener("htmx:load", function (ev) {
    initShowWhen(ev.detail.elt);
  });

  // --- Adaptive monitoring polling ---------------------------------------
  // The four monitoring fragments carry data-poll and hx-trigger="load" for
  // the first fetch only (htmx 2.0.4, src/htmx.js addTriggerHandler(): "load"
  // fires once, when the element is first processed). panel.js schedules the rest: 5 s while the operator
  // is active on the page, 30 s when the tab is visible but idle, and nothing
  // while the tab is hidden. hx-trigger="every Ns [expr]" could express some
  // of that, but the filter is evaluated with `new Function`, which the
  // panel's CSP (default-src 'self', no 'unsafe-eval') would break: htmx
  // 2.0.4, src/htmx.js maybeGenerateConditional() compiles the filter with
  // Function(); the EvalError is caught, logged as htmx:syntax:error, and the
  // trigger is kept without its filter, so it would fire on every tick.
  var pollActiveMs = 5000;
  var pollIdleMs = 30000;
  // A refresh that failed - the panel restarting, the network gone, an
  // error answer - is tried again, later each time: the wait doubles with
  // each failure in a row, up to a minute, and is back to normal after the
  // first refresh that works (audit 2026-10-12, M11).
  var pollMaxMs = 60000;
  var pollFailures = Object.create(null);
  // No pointer/keyboard/scroll input for this long → treat the tab as idle.
  var userIdleMs = 30000;
  var lastActivity = Date.now();
  var pollTimers = Object.create(null);

  ["mousedown", "mousemove", "keydown", "scroll", "touchstart"].forEach(function (evt) {
    document.addEventListener(evt, function () {
      lastActivity = Date.now();
    }, { passive: true });
  });

  function pollDelayMs(id) {
    var base = Date.now() - lastActivity < userIdleMs ? pollActiveMs : pollIdleMs;
    var n = pollFailures[id] || 0;
    return n ? Math.min(base * Math.pow(2, n), pollMaxMs) : base;
  }

  // The box is the request's source, so htmx fires the request's events on
  // it and not on the body (htmx 2.0.4, src/htmx.js ajaxHelper():
  // issueAjaxRequest(verb, path, resolveTarget(context.source), ...), and
  // with no source issueAjaxRequest() falls back to the document's body).
  function triggerPoll(el) {
    var done = htmx.ajax("GET", el.getAttribute("hx-get"), {
      source: el,
      target: "#" + el.id,
      swap: el.getAttribute("hx-swap") || "outerHTML"
    });
    // A failed refresh is handled by htmx:afterRequest below; the promise
    // htmx.ajax returns is rejected too, and is not to be left unhandled.
    if (done && done.catch) {
      done.catch(function () {});
    }
  }

  // A request that never ends - a panel stopped mid-answer, a proxy holding
  // it - would end no refresh, and no next one would be scheduled: htmx's
  // default timeout is none (htmx 2.0.4, src/htmx.js: config.timeout 0, and
  // issueAjaxRequest() takes requestConfig.timeout from it when neither the
  // call nor hx-request says one; xhr.timeout = requestConfig.timeout). Thirty seconds is twice the
  // slowest page the panel draws, one that asks DNS; then htmx:timeout,
  // and htmx:afterRequest with it.
  htmx.config.timeout = 30000;

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
    }, pollDelayMs(id));
  }

  document.body.addEventListener("htmx:afterSwap", function (ev) {
    var el = ev.detail.elt;
    if (!el || !el.hasAttribute || !el.hasAttribute("data-poll")) {
      return;
    }
    // Swapped-in markup still carries hx-trigger="load"; strip it so htmx does
    // not issue a duplicate GET on top of the response we just received.
    // htmx 2.0.4, src/htmx.js swap(): htmx:afterSwap fires on the new
    // elements before doSettle() runs the settle tasks, and only those
    // (makeAjaxLoadTask -> processNode) act on hx-trigger.
    el.removeAttribute("hx-trigger");
  });

  // The next refresh is scheduled when a request of the box ends, however
  // it ended. htmx 2.0.4, src/htmx.js issueAjaxRequest(): htmx:afterRequest
  // is fired on load, error, abort and timeout alike, and after a swap that
  // took the box out of the page it is fired again on the nearest parent
  // still in it - the event that reaches the body names the box by its
  // target. A refresh that ended in an error answer is not successful
  // (handleAjaxResponse(): responseInfo.successful = !isError); one cut off
  // never got that far.
  document.body.addEventListener("htmx:afterRequest", function (ev) {
    var target = ev.detail.target;
    if (!target || !target.id) {
      return;
    }
    var el = document.getElementById(target.id);
    if (!el || !el.hasAttribute("data-poll")) {
      return;
    }
    if (ev.detail.successful) {
      delete pollFailures[el.id];
    } else {
      pollFailures[el.id] = (pollFailures[el.id] || 0) + 1;
    }
    schedulePoll(el);
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
