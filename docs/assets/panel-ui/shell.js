/* Shared chrome for panel UI page mockups. Each screen is its own HTML file;
   this script injects radios, gallery, nav, sprite, and the help drawer so
   file:// viewing does not need a module fetch. */
(function () {
  if (document.body.dataset.shell === "1") return;
  document.body.dataset.shell = "1";
  var main = document.querySelector("main");
  if (!main) return;

  var page = document.body.getAttribute("data-page") || "";
  var navKey = document.body.getAttribute("data-nav") || page;
  var title = document.body.getAttribute("data-title") || document.title;
  var globalOnly = document.body.getAttribute("data-global-only") === "1";

  document.body.insertAdjacentHTML("afterbegin",
    '<input class="ctrl" type="radio" name="role" id="role-global" checked>' +
    '<input class="ctrl" type="radio" name="role" id="role-domain">' +
    '<input class="ctrl" type="radio" name="vp" id="vp-desktop" checked>' +
    '<input class="ctrl" type="radio" name="vp" id="vp-phone">' +
    '<input class="ctrl" type="radio" name="theme" id="theme-light" checked>' +
    '<input class="ctrl" type="radio" name="theme" id="theme-dark">' +
    '<input class="ctrl" type="checkbox" id="feat-inbound" checked>' +
    '<input class="ctrl" type="checkbox" id="nav-open">' +
    '<input class="ctrl" type="radio" name="help" id="help-off" checked>' +
    '<input class="ctrl" type="radio" name="help" id="help-index">' +
    '<input class="ctrl" type="radio" name="help" id="help-status">' +
    '<input class="ctrl" type="radio" name="help" id="help-password">' +
    '<input class="ctrl" type="radio" name="help" id="help-dns">' +
    '<input class="ctrl" type="radio" name="help" id="help-records">' +
    '<input class="ctrl" type="radio" name="help" id="help-dmarc">' +
    '<input class="ctrl" type="radio" name="help" id="help-connection">' +
    '<input class="ctrl" type="radio" name="help" id="help-apps">' +
    '<input class="ctrl" type="radio" name="help" id="help-domain-settings">' +
    '<input class="ctrl" type="radio" name="help" id="help-export">'
  );

  var q = new URLSearchParams(location.search);
  if (q.get("view") === "phone") document.getElementById("vp-phone").checked = true;
  if (q.get("role") === "domain") document.getElementById("role-domain").checked = true;
  if (q.get("theme") === "dark") document.getElementById("theme-dark").checked = true;
  if (q.get("inbound") === "0") document.getElementById("feat-inbound").checked = false;

  var gallery =
    '<header class="gallery">' +
      '<a class="brand-mini" href="index.html">Макеты</a>' +
      '<div class="seg"><span>Роль</span>' +
        '<label for="role-global">Global</label>' +
        '<label for="role-domain">Domain-admin</label></div>' +
      '<div class="seg"><span>Ширина</span>' +
        '<label for="vp-desktop">Desktop</label>' +
        '<label for="vp-phone">Phone</label></div>' +
      '<div class="seg"><span>Тема</span>' +
        '<label for="theme-light">Light</label>' +
        '<label for="theme-dark">Dark</label></div>' +
      '<label class="g-only" for="feat-inbound">Inbound</label>' +
      '<a href="index.html">Оглавление</a>' +
      '<a href="system.html">Система</a>' +
    "</header>";

  var sprite =
    '<svg class="sprite" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">' +
      '<symbol id="i-status" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M1.25 8.5h2.9L6.2 3.4l3.1 9.4 1.9-4.3h3.55"/></symbol>' +
      '<symbol id="i-domains" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="8" cy="8" r="6.25"/><path d="M1.9 8h12.2"/><path d="M8 1.75c1.85 1.8 2.8 4 2.8 6.25S9.85 12.45 8 14.25C6.15 12.45 5.2 10.25 5.2 8S6.15 3.55 8 1.75Z"/></symbol>' +
      '<symbol id="i-deliveries" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M14.25 1.75 1.6 6.6l5 2.05 2.05 5z"/><path d="M14.25 1.75 6.6 8.65"/></symbol>' +
      '<symbol id="i-queue" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M1.75 9.5h3.3l1 1.75h3.9l1-1.75h3.3v3.05a1.2 1.2 0 0 1-1.2 1.2H2.95a1.2 1.2 0 0 1-1.2-1.2z"/><path d="M1.75 9.5 3.4 3.2a1.25 1.25 0 0 1 1.2-.95h6.8a1.25 1.25 0 0 1 1.2.95l1.65 6.3"/></symbol>' +
      '<symbol id="i-log" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M3.75 1.75h5.1l3.4 3.4v8.05a1.05 1.05 0 0 1-1.05 1.05H3.75a1.05 1.05 0 0 1-1.05-1.05V2.8a1.05 1.05 0 0 1 1.05-1.05Z"/><path d="M8.85 1.75v3.4h3.4"/><path d="M5.35 8.6h5.3M5.35 11.1h3.5"/></symbol>' +
      '<symbol id="i-inbound" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M2.5 9.5h11"/><path d="M8 2.75v6.2"/><path d="M5.4 6.4 8 9.05 10.6 6.4"/><path d="M3.2 12.6h9.6"/></symbol>' +
      '<symbol id="i-dmarc" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M8 1.85 2.75 3.7v4.2c0 3.15 2.15 5.2 5.25 6.25 3.1-1.05 5.25-3.1 5.25-6.25V3.7Z"/><path d="M5.4 8.05 7.15 9.8 10.7 6.2"/></symbol>' +
      '<symbol id="i-backup" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="2.75" y="1.75" width="10.5" height="12.5" rx="1.15"/><path d="M2.75 8h10.5"/><path d="M6.4 4.85h3.2M6.4 11.15h3.2"/></symbol>' +
      '<symbol id="i-users" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M10.9 3.1a2.1 2.1 0 0 1 0 4.2"/><path d="M14.7 14.25a3.8 3.8 0 0 0-3.9-3.65"/><circle cx="5.5" cy="5.2" r="2.5"/><path d="M1.4 14.25a4.8 4.8 0 0 1 8.2 0"/></symbol>' +
      '<symbol id="i-help" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="8" cy="8" r="6.25"/><path d="M8 7.2V11.4"/><path d="M8 5.05v.01"/></symbol>' +
      '<symbol id="i-settings" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.25" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06A1.65 1.65 0 0 0 4.68 15a1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06A1.65 1.65 0 0 0 19.4 9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z"/></symbol>' +
      '<symbol id="i-account" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="8" cy="5.4" r="2.75"/><path d="M2.9 14.25a5.1 5.1 0 0 1 10.2 0"/></symbol>' +
      '<symbol id="i-out" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M6.1 14.25H3.65a1.15 1.15 0 0 1-1.15-1.15V2.9a1.15 1.15 0 0 1 1.15-1.15H6.1"/><path d="M10.6 11.15 13.75 8 10.6 4.85"/><path d="M13.75 8H6.35"/></symbol>' +
      '<symbol id="i-menu" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><path d="M2.5 4h11M2.5 8h11M2.5 12h11"/></symbol>' +
    "</svg>";

  function icon(id) {
    return '<svg class="icon"><use href="#' + id + '"/></svg>';
  }

  var navHtml =
    '<nav class="nav">' +
      '<a class="brand" href="status.html"><img src="../selfpost-stamp-compact.svg" width="220" height="100" alt="SelfPost"></a>' +
      '<div class="links">' +
        '<a class="n-status g-only" href="status.html">' + icon("i-status") + "Status</a>" +
        '<a class="n-domains" href="domains.html">' + icon("i-domains") + "Domains</a>" +
        '<a class="n-deliveries" href="deliveries.html">' + icon("i-deliveries") + "Deliveries</a>" +
        '<a class="n-queue g-only" href="mail-queue.html">' + icon("i-queue") + "Mail queue</a>" +
        '<a class="n-log g-only" href="system-log.html">' + icon("i-log") + "System log</a>" +
        '<a class="n-inbound g-only in-only" href="inbound.html">' + icon("i-inbound") + "Inbound</a>" +
        '<a class="n-dmarc g-only" href="dmarc.html">' + icon("i-dmarc") + 'DMARC <span class="tag future">1.x</span></a>' +
        '<a class="n-backup g-only" href="backup.html">' + icon("i-backup") + "Backup</a>" +
        '<a class="n-users g-only" href="users.html">' + icon("i-users") + "Users</a>" +
        '<a class="n-help" href="help.html">' + icon("i-help") + 'Help <span class="tag future">1.x</span></a>' +
      "</div>" +
      '<div class="session">' +
        '<span class="session-user muted">' + icon("i-account") + "User: admin</span>" +
        '<a class="n-settings" href="settings.html">' + icon("i-settings") + "Settings</a>" +
        '<a class="btn-ghost" href="login.html">' + icon("i-out") + "Sign out</a>" +
      "</div>" +
    "</nav>";

  var phone =
    '<div class="phone-bar">' +
      '<label class="nav-burger" for="nav-open" title="Menu">' + icon("i-menu") + "</label>" +
      '<img class="phone-mark" src="../selfpost-icon.svg" width="28" height="28" alt="">' +
      '<span class="grow phone-title"></span>' +
      '<label class="help-link" for="help-index" title="Help">?</label>' +
    "</div>";

  var help =
    '<label class="help-scrim" for="help-off"></label>' +
    '<aside class="help-drawer">' +
      '<label class="help-close" for="help-off">Close</label>' +
      '<article class="help-pane help-pane-index">' +
        "<h2>Help</h2>" +
        "<p>Short notes for the card you opened — not a second copy of the guide.</p>" +
        '<p class="muted">Status</p>' +
        '<ul class="toc"><li><label for="help-status">Status checks</label></li></ul>' +
        '<p class="muted">Domain</p>' +
        '<ul class="toc">' +
          '<li><label for="help-password">New application password</label></li>' +
          '<li><label for="help-dns">DNS status</label></li>' +
          '<li><label for="help-records">DKIM and SPF records</label></li>' +
          '<li><label for="help-dmarc">DMARC record</label></li>' +
          '<li><label for="help-connection">Connection settings</label></li>' +
          '<li><label for="help-apps">Applications</label></li>' +
          '<li><label for="help-domain-settings">Domain settings</label></li>' +
          '<li><label for="help-export">Export domain</label></li>' +
        "</ul></article>" +
      '<article class="help-pane help-pane-status">' +
        "<h2>Status checks</h2>" +
        "<p>The cards keep the readings. This drawer is what used to sit under them as paragraphs.</p>" +
        "<h2>Machine</h2>" +
        "<p>CPU and memory are the container’s readings. Network is a short rate window. These numbers explain load; they do not replace the queue.</p>" +
        "<h2>TLS certificate</h2>" +
        "<p>Presented on port 465. Issued and renewed outside SelfPost. Warn = expires soon; error = missing, and submission will fail.</p>" +
        "<h2>Hostname / reverse DNS</h2>" +
        "<p>The hostname must forward to this IP and the PTR must come back to the same name. Set PTR at the provider.</p>" +
        "<h2>Mail queue</h2>" +
        "<p>Deferred mail is retried on a time schedule (first delay, doubling cap, queue lifetime). There is no “attempt 3 of N”.</p>" +
        '<p class="more muted"><label for="help-index">All topics</label></p></article>' +
      '<article class="help-pane help-pane-password">' +
        "<h2>New application password</h2>" +
        "<p>Shown <strong>once only</strong> and not stored. Copy it now — if it is lost, regenerate a new one. The previous password stops working immediately.</p>" +
        '<p class="more muted"><label for="help-index">All topics</label></p></article>' +
      '<article class="help-pane help-pane-dns">' +
        "<h2>DNS status</h2>" +
        "<p>The badge is the worst of DKIM, SPF and DMARC. Results are cached a few minutes — use <em>Re-check</em> after publishing.</p>" +
        "<p>SPF is a shallow check: the literal address only, no <code>include:</code> or <code>redirect=</code>. Report authorization is required only when <code>rua=</code> points at a domain this server does not accept.</p>" +
        '<p class="more muted"><label for="help-index">All topics</label></p></article>' +
      '<article class="help-pane help-pane-records">' +
        "<h2>DKIM and SPF records</h2>" +
        "<p>DKIM is not a secret. The selector on this page is the one this server signs with. Merge the SPF example into an existing record if the domain already has one — do not publish a second TXT.</p>" +
        '<p class="more muted"><label for="help-index">All topics</label></p></article>' +
      '<article class="help-pane help-pane-dmarc">' +
        "<h2>DMARC record</h2>" +
        "<p><code>p=none</code> does not affect delivery. Tighten to <code>p=quarantine</code> then <code>p=reject</code> once reports look clean. The report address is set under Domain settings (or the Settings default).</p>" +
        '<p class="more muted"><label for="help-index">All topics</label></p></article>' +
      '<article class="help-pane help-pane-connection">' +
        "<h2>Connection settings</h2>" +
        "<p>Same host for every domain. Authenticate with an application login from this page. Auth is required on every port. The password is shown once at create or regenerate.</p>" +
        "<p>465 is implicit TLS; 587 is STARTTLS submission when that port is enabled.</p>" +
        '<p class="more muted"><label for="help-index">All topics</label></p></article>' +
      '<article class="help-pane help-pane-apps">' +
        "<h2>Applications</h2>" +
        "<p>SASL logins for this domain. Login is unique across domains; letters, digits, <code>.</code>, <code>-</code> and <code>_</code>. The password is shown once.</p>" +
        "<p>Address mode is which From addresses this application may use: any address of the domain, or a fixed list. A trusted-IP override gives those clients a higher ceiling than the domain (still ≤ level 1) and skips the domain check; everyone else uses the domain limit if set, otherwise level 1.</p>" +
        '<p class="more muted"><label for="help-index">All topics</label></p></article>' +
      '<article class="help-pane help-pane-domain-settings">' +
        "<h2>Domain settings</h2>" +
        "<p>Aggregate reports (<code>rua=</code>) inherit the Settings default, or you override them per domain. Level 2 is an optional ceiling for all senders on this domain; it must be ≤ level 1. Application overrides live on each application.</p>" +
        '<p class="more muted"><label for="help-index">All topics</label></p></article>' +
      '<article class="help-pane help-pane-export">' +
        "<h2>Export domain</h2>" +
        "<p>The file is a secret: it carries the DKIM key and application passwords, so published DNS does not have to change on the other instance. Transfer it securely, or encrypt it as <code>.spde</code>.</p>" +
        '<p class="more muted"><label for="help-index">All topics</label></p></article>' +
    "</aside>";

  var app = document.createElement("div");
  app.className = "app";
  app.innerHTML = navHtml + '<div class="stage">' + phone + "</div>";
  main.parentNode.insertBefore(app, main);
  app.insertAdjacentHTML("beforebegin", gallery + sprite);
  app.querySelector(".stage").appendChild(main);
  app.insertAdjacentHTML("afterend", help);

  var cur = app.querySelector(".n-" + navKey);
  if (cur) cur.setAttribute("aria-current", "page");
  var grow = app.querySelector(".phone-title");
  if (grow) grow.textContent = title;

  var skip = { "index.html": 1, "system.html": 1, "app.html": 1 };

  function qs() {
    var p = new URLSearchParams();
    if (document.getElementById("vp-phone").checked) p.set("view", "phone");
    if (document.getElementById("role-domain").checked) p.set("role", "domain");
    if (document.getElementById("theme-dark").checked) p.set("theme", "dark");
    if (!document.getElementById("feat-inbound").checked) p.set("inbound", "0");
    var s = p.toString();
    return s ? "?" + s : "";
  }

  function withQuery(href) {
    if (!href) return href;
    if (href.charAt(0) === "#" || /^(https?:|mailto:|javascript:)/i.test(href)) return href;
    var hash = "";
    var path = href;
    var hashAt = href.indexOf("#");
    if (hashAt >= 0) {
      hash = href.slice(hashAt);
      path = href.slice(0, hashAt);
    }
    var qAt = path.indexOf("?");
    var file = qAt >= 0 ? path.slice(0, qAt) : path;
    var extra = qAt >= 0 ? path.slice(qAt + 1) : "";
    var base = file.split("/").pop();
    if (!base || !/\.html$/i.test(base) || skip[base]) return href;
    var p = new URLSearchParams(extra);
    var curQs = new URLSearchParams(qs().replace(/^\?/, ""));
    ["view", "role", "theme", "inbound"].forEach(function (k) {
      if (curQs.has(k)) p.set(k, curQs.get(k));
      else p.delete(k);
    });
    var s = p.toString();
    return base + (s ? "?" + s : "") + hash;
  }

  function rewriteLinks() {
    document.querySelectorAll("a[href]").forEach(function (a) {
      var raw = a.getAttribute("href");
      if (!raw) return;
      a.setAttribute("href", withQuery(raw));
    });
  }

  function brandHref() {
    var brand = document.querySelector(".nav .brand");
    if (!brand) return;
    brand.setAttribute("href", withQuery(
      document.getElementById("role-domain").checked ? "domains.html" : "status.html"
    ));
  }

  function gate() {
    brandHref();
    if (!document.getElementById("role-domain").checked) return;
    if (globalOnly) location.replace(withQuery("domains.html"));
  }

  function syncUrl() {
    if (history.replaceState) {
      history.replaceState(null, "", location.pathname.split("/").pop() + qs() + location.hash);
    }
    rewriteLinks();
    brandHref();
    gate();
  }

  ["role-global", "role-domain", "vp-desktop", "vp-phone", "theme-light", "theme-dark", "feat-inbound"].forEach(function (id) {
    var el = document.getElementById(id);
    if (el) el.addEventListener("change", syncUrl);
  });

  document.querySelectorAll(".nav a").forEach(function (a) {
    a.addEventListener("click", function () {
      var open = document.getElementById("nav-open");
      if (open) open.checked = false;
    });
  });

  rewriteLinks();
  gate();
})();
