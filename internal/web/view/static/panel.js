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

  // --- Address list shown only in list mode -----------------------------
  // The "Addresses" field applies to list mode only; in wildcard mode the
  // server ignores it, so hiding it removes a field that does nothing. The
  // toggle runs on load too, because the edit form of an existing application
  // may already be set to list mode.
  function syncAddressField(select) {
    var form = select.closest("form");
    var field = form && form.querySelector("[data-addresses]");
    if (!field) {
      return;
    }
    // The mode values come from the server (store.AddressModeList), so the
    // select carries the one that means "list" rather than this script
    // hard-coding it.
    field.hidden = select.value !== select.dataset.listMode;
  }

  function initAddressFields(root) {
    root.querySelectorAll("select[data-list-mode]").forEach(function (select) {
      syncAddressField(select);
      select.addEventListener("change", function () {
        syncAddressField(select);
      });
    });
  }

  // --- Encryption password fields shown only when asked for --------------
  // The backup, export and import forms carry an optional password block. It
  // is hidden until the checkbox next to it is ticked, and cleared when it is
  // unticked, so a password typed and then abandoned is never submitted. With
  // JavaScript blocked the block stays visible and the forms behave exactly as
  // the server reads them: the checkbox alone decides whether encryption
  // happens.
  function syncEncryptFields(box) {
    var form = box.closest("form");
    var fields = form && form.querySelector("[data-encrypt-fields]");
    if (!fields) {
      return;
    }
    fields.hidden = !box.checked;
    if (!box.checked) {
      fields.querySelectorAll("input").forEach(function (input) {
        input.value = "";
      });
    }
  }

  function initEncryptFields(root) {
    root.querySelectorAll("input[data-encrypt-toggle]").forEach(function (box) {
      syncEncryptFields(box);
      box.addEventListener("change", function () {
        syncEncryptFields(box);
      });
    });
  }

  // --- Import password field shown based on the chosen file's extension ---
  // The domain-import file decides for itself whether it is encrypted (the
  // server checks the envelope magic, not a checkbox), so the panel offers
  // the password field the same way: reveal it for a .spde file, hide and
  // clear it for a plain .json one. With no file chosen yet there is nothing
  // to ask a password for, so the field stays hidden until a file names it.
  // An unrecognised name leaves the field visible rather than guessing wrong
  // and hiding a password the file needs.
  function syncImportPasswordField(input) {
    var form = input.closest("form");
    var fields = form && form.querySelector("[data-import-password-fields]");
    if (!fields) {
      return;
    }
    var name = (input.files && input.files[0] && input.files[0].name || "").toLowerCase();
    var hide = name === "" || /\.json$/.test(name);
    fields.hidden = hide;
    if (hide) {
      fields.querySelectorAll("input").forEach(function (pw) {
        pw.value = "";
      });
    }
  }

  function initImportPasswordField(root) {
    root.querySelectorAll("input[data-import-file]").forEach(function (input) {
      syncImportPasswordField(input);
      input.addEventListener("change", function () {
        syncImportPasswordField(input);
      });
    });
  }

  // --- Section index follows the page -----------------------------------
  // The long pages list their own sections in the navigation column (the
  // "sections" template). Marking the one currently in view turns that list
  // from an index into a position, which is the whole point of it on a page
  // nine cards tall. The links work without any of this; only the highlight
  // depends on it.
  //
  // Each pass looks its targets up by id rather than holding on to elements
  // found once: the status page replaces its cards wholesale every five
  // seconds (adaptive polling on #status-body), and anything remembered here would be
  // measuring boxes that had left the document.
  var sectionLinks = [];

  function markCurrentSection() {
    var current = null;
    sectionLinks.forEach(function (link) {
      var target = document.getElementById(link.hash.slice(1));
      // The section in view is the last one whose top has passed the reading
      // line; the links are in document order, so the last match wins.
      if (target && target.getBoundingClientRect().top <= 100) {
        current = link;
      }
    });
    if (window.innerHeight + window.scrollY >= document.documentElement.scrollHeight - 2) {
      // At the foot of the page there is no scroll left to bring the last
      // cards up to the reading line, so without this they could never be
      // marked however far down you are — and the last card of the domain
      // page is the one that deletes it.
      current = sectionLinks[sectionLinks.length - 1];
    } else if (!current) {
      // Above the first heading nothing has been passed yet, and the page is
      // still on its first section.
      current = sectionLinks[0];
    }
    sectionLinks.forEach(function (link) {
      link.classList.toggle("current", link === current);
    });
  }

  function initSectionIndex() {
    sectionLinks = Array.prototype.slice.call(
      document.querySelectorAll(".sections a[href^='#']")
    );
    if (!sectionLinks.length) {
      return;
    }
    var pending = false;
    // Scroll fires far more often than the highlight can change, so the work
    // is collapsed onto the next frame.
    window.addEventListener("scroll", function () {
      if (pending) {
        return;
      }
      pending = true;
      window.requestAnimationFrame(function () {
        pending = false;
        markCurrentSection();
      });
    }, { passive: true });
    markCurrentSection();
  }

  document.addEventListener("DOMContentLoaded", function () {
    initAddressFields(document);
    initEncryptFields(document);
    initImportPasswordField(document);
    initSectionIndex();
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
