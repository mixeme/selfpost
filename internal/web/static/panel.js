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

  document.addEventListener("DOMContentLoaded", function () {
    initAddressFields(document);
    initEncryptFields(document);
  });
})();
