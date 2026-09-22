/* Rendered-page audit for the panel: what check.py cannot see because it needs
   a browser. Open any page of the mockups (or of the running panel), paste this
   file into the console, then:

     await panelAudit(["overview.html", "out-domain.html", ...])   // same-origin URLs
     await panelAudit()                                            // the page you are on

   It loads each page in a 1440px and a 1280px frame and reports
     - text under the 13px floor (the postmark's imprint excepted),
     - text under 4.5:1 against the ground it actually sits on,
     - anything wider than its box, and tables that have to scroll,
     - health-card values that got clipped.
   An empty `problems` list is the pass. Attach the output to the step's evidence
   (docs/plans/panel-redesign.md, Enforcement). */
async function panelAudit(urls = [location.href], widths = [1440, 1280]) {
  const lum = (c) => {
    const [r, g, b] = c.map((v) => ((v /= 255) <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4));
    return 0.2126 * r + 0.7152 * g + 0.0722 * b;
  };
  const rgba = (s) => { const m = s.match(/[\d.]+/g).map(Number); return { rgb: m.slice(0, 3), a: m.length > 3 ? m[3] : 1 }; };
  const ground = (el, win) => {
    for (let e = el; e; e = e.parentElement) { const b = rgba(win.getComputedStyle(e).backgroundColor); if (b.a > 0.5) return b.rgb; }
    return [255, 255, 255];
  };
  const problems = new Set();
  let texts = 0;
  for (const url of urls) for (const width of widths) {
    const f = document.createElement("iframe");
    f.width = width; f.height = 900; f.src = url;
    document.body.appendChild(f);
    await new Promise((r) => (f.onload = r));
    await f.contentDocument.fonts.ready;
    await new Promise((r) => setTimeout(r, 150));
    const d = f.contentDocument, win = f.contentWindow, at = `${width} ${url.split("/").pop()}`;
    const walker = d.createTreeWalker(d.body, NodeFilter.SHOW_TEXT);
    for (let n; (n = walker.nextNode()); ) {
      const text = n.textContent.trim(), el = n.parentElement;
      if (!text || el.closest(".sp-log")) continue;   // the log pane has its own dark ground and palette
      texts++;
      const cs = win.getComputedStyle(el), what = `${el.tagName.toLowerCase()}.${[...el.classList].join(".")} "${text.slice(0, 24)}"`;
      if (parseFloat(cs.fontSize) < 12.9 && !el.closest(".sp-postmark")) problems.add(`${at}: ${parseFloat(cs.fontSize)}px ${what}`);
      const fg = rgba(cs.color).rgb, bg = ground(el, win), a = lum(fg), b = lum(bg);
      const ratio = (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05);
      const large = parseFloat(cs.fontSize) >= 24 || (parseFloat(cs.fontSize) >= 18.6 && +cs.fontWeight >= 600);
      if (ratio < (large ? 3 : 4.5)) problems.add(`${at}: contrast ${ratio.toFixed(2)} ${what}`);
    }
    if (d.documentElement.scrollWidth > win.innerWidth) problems.add(`${at}: the page scrolls sideways`);
    for (const e of d.querySelectorAll(".box, .sp-head, .sp-box-head")) if (e.scrollWidth > e.clientWidth + 1) problems.add(`${at}: overflow in .${e.className}`);
    for (const e of d.querySelectorAll(".table-container")) if (e.scrollWidth > e.clientWidth + 1) problems.add(`${at}: a table scrolls by ${e.scrollWidth - e.clientWidth}px`);
    for (const e of d.querySelectorAll(".sp-h-name, .sp-h-value, .sp-h-sub")) if (e.scrollWidth > e.clientWidth + 1) problems.add(`${at}: clipped "${e.textContent}"`);
    for (const i of d.querySelectorAll("i.ti")) if (win.getComputedStyle(i, "::before").content === "none") problems.add(`${at}: missing icon ${i.className}`);
    f.remove();
  }
  return { pages: urls.length, widths, textsChecked: texts, problems: [...problems] };
}
