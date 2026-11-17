/* GoShiSen-Enforce docs site — static SPA.
 * - Nav + page list come from manifest.json (generated from the repo).
 * - Language: EN (repo markdown) / VI (docs-site/translations/vi/
 *   mirror; falls back to EN with a notice when a translation is missing).
 * - Theme: Catppuccin Latte (light) / Mocha (dark); auto by system,
 *   manual override stored in localStorage.
 */
(function () {
  "use strict";

  var VERSION = "1.0.0";
  var LANG_KEY = "gsh-docs-lang";
  var THEME_KEY = "gsh-docs-theme";

  var state = {
    lang: "en",
    theme: null, // "light" | "dark" | "auto"
    manifest: [],
    pages: {},
    current: null,
    i18n: {}
  };

  /* ── tiny helpers ──────────────────────────────────── */
  function $(id) { return document.getElementById(id); }
  function listen(id, ev, fn) { var el = $(id); if (el) el.addEventListener(ev, fn); }

  /* Inline SVG icons (stroke, currentColor — follow the Catppuccin theme). */
  var ICONS = {
    root: '<svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.9l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.9-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1-1.6 1.7 1.7 0 0 0-1.9.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.9 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.6-1 1.7 1.7 0 0 0-.3-1.9l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.9.3h0a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.9-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.9v0a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z"/></svg>',
    docs: '<svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M2 4h6a4 4 0 0 1 4 4v12a3 3 0 0 0-3-3H2z"/><path d="M22 4h-6a4 4 0 0 0-4 4v12a3 3 0 0 1 3-3h7z"/></svg>',
    design: '<svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><circle cx="12" cy="12" r="9"/><polygon points="16.24 7.76 14.12 14.12 7.76 16.24 9.88 9.88 16.24 7.76"/></svg>',
    release: '<svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M20.6 13.4 13.4 20.6a2 2 0 0 1-2.8 0L3 13V3h10z"/><circle cx="7.5" cy="7.5" r="1.5"/></svg>',
    home: '<svg class="icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" aria-hidden="true"><path d="M3 10.5 12 3l9 7.5"/><path d="M5 9.5V21h14V9.5"/></svg>'
  };

  function fetchText(url) {
    return fetch(url, { cache: "no-store" }).then(function (r) {
      if (!r.ok) throw new Error("HTTP " + r.status);
      return r.text();
    });
  }

  /* ── i18n ──────────────────────────────────────────── */
  function t(key) { return state.i18n[key] || key; }

  /* ── manifest ──────────────────────────────────────── */
  function loadManifest() {
    return fetchText("manifest.json").then(function (txt) {
      state.manifest = JSON.parse(txt);
      state.manifest.forEach(function (d) { state.pages[d.slug] = d; });
    });
  }

  function loadI18n() {
    return fetchText("i18n/" + state.lang + ".json").then(function (txt) {
      state.i18n = JSON.parse(txt);
      document.documentElement.lang = state.lang;
      document.title = state.i18n.site_title || "GoShiSen-Enforce Docs";
    });
  }

  /* ── translation resolution ────────────────────────── */
  function viPath(doc) {
    // "../README.md" -> "translations/vi/README.md"
    var rel = doc.path.replace(/^\.\.\//, "");
    return "translations/vi/" + rel;
  }

  function docUrl(doc) {
    if (state.lang === "vi") return viPath(doc);
    return doc.path;
  }

  function loadDoc(doc) {
    if (state.lang !== "vi") {
      return fetchText(doc.path).then(function (txt) {
        return { text: txt, translated: false };
      });
    }
    // Vietnamese requested: try translation, fall back to EN.
    return fetchText(viPath(doc)).then(function (txt) {
      return { text: txt, translated: true };
    }, function () {
      return fetchText(doc.path).then(function (txt) {
        return { text: txt, translated: false };
      });
    });
  }

  /* ── nav tree ──────────────────────────────────────── */
  var GROUPS = [
    { key: "root", labelKey: "nav_meta" },
    { key: "docs", labelKey: "nav_user" },
    { key: "design", labelKey: "nav_design" },
    { key: "release", labelKey: "nav_release" }
  ];

  // Group by the manifest's real path (../docs/design/…, ../docs/release/…),
  // not by slug prefix — "docs-release-checklist" is a top-level doc.
  function docGroup(d) {
    var p = d.path;
    if (p.indexOf("../docs/design/") === 0) return "design";
    if (p.indexOf("../docs/release/") === 0) return "release";
    if (p.indexOf("../docs/") === 0) return "docs";
    return "root";
  }

  function groupLabel(key) {
    for (var i = 0; i < GROUPS.length; i++) if (GROUPS[i].key === key) return (ICONS[key] || "") + t(GROUPS[i].labelKey);
    return key;
  }

  function renderNav() {
    var sidebar = $("sidebar");
    var html = '<div class="brand"><span class="logo">G</span>' +
      '<span>' + t("site_title") + '<span class="tagline">' + t("tagline") + "</span></span></div>";
    html += '<nav class="nav">';

    var groups = {};
    state.manifest.forEach(function (d) {
      var g = docGroup(d);
      (groups[g] = groups[g] || []).push(d);
    });

    GROUPS.forEach(function (grp) {
      var list = groups[grp.key];
      if (!list) return;
      html += "<section><h4>" + groupLabel(grp.key) + "</h4>";
      list.forEach(function (d) {
        var active = state.current === d.slug ? ' class="active"' : "";
        html += '<a href="#' + encodeURIComponent(d.slug) + '"' + active + " data-slug=\"" + d.slug + "\">" +
          escapeHtml(d.title) + "</a>";
      });
      html += "</section>";
    });
    html += "</nav>";
    sidebar.innerHTML = html;

    sidebar.querySelectorAll(".nav a").forEach(function (a) {
      a.addEventListener("click", function (e) {
        e.preventDefault();
        state.current = a.getAttribute("data-slug");
        location.hash = "#" + encodeURIComponent(state.current);
        renderNav();
        renderDoc();
        sidebar.classList.remove("open");
        window.scrollTo(0, 0);
      });
    });
  }

  function escapeHtml(s) {
    return String(s).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  }

  /* ── render ────────────────────────────────────────── */
  function renderHome() {
    var html = '<div class="home"><h1>' + ICONS.home + " " + t("site_title") + "</h1>";
    html += '<p class="lead">' + t("banner") + "</p><div class=\"cards\">";
    var core = ["readme", "docs-developer-guide", "docs-user-guide", "docs-installation",
      "docs-openwrt", "docs-release-checklist", "docs-changelog",
      "docs-release-notes-v0-2-0", "docs-release-notes-v0-1-0"];
    core.forEach(function (slug) {
      var d = state.pages[slug];
      if (!d) return;
      var g = docGroup(d);
      html += '<a class="card" href="#' + encodeURIComponent(slug) + '">' +
        '<span class="card-icon">' + (ICONS[g] || "") + "</span>" +
        "<h3>" + escapeHtml(d.title) + "</h3><p>" + escapeHtml(slug) + "</p></a>";
    });
    html += "</div></div>";
    return html;
  }

  function renderDoc() {
    var content = $("content");
    var doc = state.pages[state.current];
    if (!doc) { content.innerHTML = "<p>" + t("not_found") + "</p>"; return; }

    loadDoc(doc).then(function (res) {
      content.innerHTML = '<article class="md">' + window.mdRender(res.text) + "</article>";
      if (!res.translated && state.lang === "vi") {
        var w = document.createElement("div");
        w.style.cssText = "background:var(--surface);border-left:4px solid var(--ctp-peach);padding:8px 14px;border-radius:0 6px 6px 0;margin:1em 0;";
        w.textContent = t("pending_note");
        content.insertBefore(w, content.firstChild);
      }
    }, function () {
      content.innerHTML = "<p>" + t("load_error") + "</p>";
    });
  }

  /* ── theme ─────────────────────────────────────────── */
  function applyTheme() {
    var theme = state.theme;
    if (theme === "auto" || theme === null) {
      theme = window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
    }
    document.documentElement.setAttribute("data-theme", theme);
    var icon = $("theme-icon");
    var label = $("theme-label");
    if (icon) icon.textContent = theme === "dark" ? "🌙" : "☀️";
    if (label) label.textContent = theme === "dark" ? t("theme_dark") : t("theme_light");
  }

  function initTheme() {
    state.theme = localStorage.getItem(THEME_KEY) || "auto";
    applyTheme();
    window.matchMedia("(prefers-color-scheme: dark)").addEventListener("change", applyTheme);
    listen("theme-toggle", "click", function () {
      state.theme = state.theme === "auto" || state.theme === "light" ? "dark" : "light";
      localStorage.setItem(THEME_KEY, state.theme);
      applyTheme();
    });
  }

  /* ── lang switch ───────────────────────────────────── */
  function renderLangSwitch() {
    var box = $("lang-switch");
    box.innerHTML = '<button data-lang="en" class="' + (state.lang === "en" ? "active" : "") +
      '">' + t("lang_en") + '</button><button data-lang="vi" class="' +
      (state.lang === "vi" ? "active" : "") + '">' + t("lang_vi") + "</button>";
    box.querySelectorAll("button").forEach(function (b) {
      b.addEventListener("click", function () {
        var lang = b.getAttribute("data-lang");
        if (lang === state.lang) return;
        state.lang = lang;
        localStorage.setItem(LANG_KEY, lang);
        loadI18n().then(function () {
          renderLangSwitch();
          renderNav();
          if (state.current === "home") renderPage(); else renderDoc();
          applyTheme();
        });
      });
    });
  }

  /* ── router ────────────────────────────────────────── */
  function parseHash() {
    var h = location.hash.replace(/^#/, "");
    try { h = decodeURIComponent(h); } catch (e) {}
    return h || null;
  }

  function renderPage() {
    var slug = parseHash();
    if (!slug) { state.current = "home"; $("content").innerHTML = renderHome(); return; }
    if (!state.pages[slug]) { state.current = null; $("content").innerHTML = "<p>" + t("not_found") + "</p>"; return; }
    state.current = slug;
    renderDoc();
  }

  /* ── boot ──────────────────────────────────────────── */
  function boot() {
    var savedLang = localStorage.getItem(LANG_KEY);
    state.lang = savedLang === "vi" ? "vi" : "en";

    loadManifest()
      .then(loadI18n)
      .then(function () {
        initTheme();
        renderNav();
        renderLangSwitch();
        renderPage();
        listen("nav-toggle", "click", function () {
          $("sidebar").classList.toggle("open");
        });
        window.addEventListener("hashchange", function () { renderPage(); });
      })
      .catch(function (err) {
        $("content").innerHTML = "<p>Failed to load site data: " + escapeHtml(err.message) + "</p>";
      });
  }

  document.addEventListener("DOMContentLoaded", boot);
})();
