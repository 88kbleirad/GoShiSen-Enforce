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
    { key: "root", labelKey: "nav_meta", match: /^(readme|contributing|security)$/ },
    { key: "docs", labelKey: "nav_user", match: /^docs-/ },
    { key: "design", labelKey: "nav_design", match: /^docs-design-/ },
    { key: "release", labelKey: "nav_release", match: /^docs-release-/ }
  ];

  function groupLabel(key) {
    for (var i = 0; i < GROUPS.length; i++) if (GROUPS[i].key === key) return t(GROUPS[i].labelKey);
    return key;
  }

  function renderNav() {
    var sidebar = $("sidebar");
    var html = '<div class="brand"><span class="logo">G</span>' +
      '<span>' + t("site_title") + '<span class="tagline">' + t("tagline") + "</span></span></div>";
    html += '<nav class="nav">';

    var groups = {};
    state.manifest.forEach(function (d) {
      var g = "docs";
      for (var i = 0; i < GROUPS.length; i++) {
        if (GROUPS[i].match.test(d.slug)) { g = GROUPS[i].key; break; }
      }
      (groups[g] = groups[g] || []).push(d);
    });

    Object.keys(groups).forEach(function (g) {
      html += "<section><h4>" + groupLabel(g) + "</h4>";
      groups[g].forEach(function (d) {
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
    var html = '<div class="home"><h1>' + t("site_title") + "</h1>";
    html += '<p class="lead">' + t("banner") + "</p><div class=\"cards\">";
    var core = ["readme", "docs-developer-guide", "docs-user-guide", "docs-installation",
      "docs-openwrt", "docs-release-checklist", "docs-changelog",
      "docs-release-notes-v0-2-0", "docs-release-notes-v0-1-0"];
    core.forEach(function (slug) {
      var d = state.pages[slug];
      if (!d) return;
      html += '<a class="card" href="#' + encodeURIComponent(slug) + '"><h3>' +
        escapeHtml(d.title) + "</h3><p>" + escapeHtml(slug) + "</p></a>";
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
