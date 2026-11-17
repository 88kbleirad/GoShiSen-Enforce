/* docs-site markdown renderer — self-contained, escape-first, no deps.
 * Supports the subset used by GoShiSen-Enforce docs: ATX headings,
 * fenced code blocks, GFM tables, lists (nested 2-space), blockquotes,
 * hr, paragraphs, inline code/strong/em/links. HTML in source is
 * escaped, never passed through.
 */
(function (global) {
  "use strict";

  function esc(s) {
    return String(s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  function slugify(s) {
    return String(s)
      .toLowerCase()
      .replace(/[^\w\s-]/g, "")
      .trim()
      .replace(/\s+/g, "-");
  }

  function inline(s) {
    // escape first, then apply markdown spans in safe order
    s = esc(s);
    s = s.replace(/`([^`]+)`/g, function (m, c) { return "<code>" + c + "</code>"; });
    s = s.replace(/\*\*([^*]+)\*\*/g, "<strong>$1</strong>");
    s = s.replace(/(^|[^*])\*([^*\n]+)\*(?!\*)/g, "$1<em>$2</em>");
    s = s.replace(/\[([^\]]+)\]\(([^)\s]+)(?:\s+"[^"]*")?\)/g, '<a href="$2">$1</a>');
    return s;
  }

  function isTableSep(line) {
    return /^\s*\|?[\s:|-]+\|?\s*$/.test(line) && line.indexOf("-") !== -1;
  }

  function parseTable(lines, i) {
    var cells = function (line) {
      var parts = line.trim().replace(/^\|/, "").replace(/\|$/, "").split("|");
      var out = [];
      for (var k = 0; k < parts.length; k++) out.push(parts[k].trim());
      return out;
    };
    var header = cells(lines[i]);
    var body = [];
    var j = i + 2; // skip separator
    while (j < lines.length && lines[j].indexOf("|") !== -1 && lines[j].trim() !== "") {
      body.push(cells(lines[j]));
      j++;
    }
    var h = '<table><thead><tr>';
    for (var n = 0; n < header.length; n++) h += "<th>" + inline(header[n]) + "</th>";
    h += "</tr></thead><tbody>";
    for (var r = 0; r < body.length; r++) {
      h += "<tr>";
      for (var c = 0; c < header.length; c++) h += "<td>" + inline(body[r][c] || "") + "</td>";
      h += "</tr>";
    }
    return { html: h + "</tbody></table>", next: j };
  }

  function render(src) {
    src = String(src || "").replace(/\r\n?/g, "\n");
    var lines = src.split("\n");
    var html = "";
    var i = 0;
    var inList = false;
    var listType = null;
    var listDepth = 0;

    function closeList() {
      if (inList) { html += "</" + listType + ">\n"; inList = false; listType = null; listDepth = 0; }
    }

    while (i < lines.length) {
      var line = lines[i];

      // fenced code block
      if (/^```/.test(line)) {
        closeList();
        var lang = line.slice(3).trim();
        var buf = [];
        i++;
        while (i < lines.length && !/^```/.test(lines[i])) { buf.push(lines[i]); i++; }
        i++; // closing fence
        html += '<pre class="code-block"><code' +
          (lang ? ' data-lang="' + esc(lang) + '"' : "") +
          ">" + esc(buf.join("\n")) + "</code></pre>\n";
        continue;
      }

      // table
      if (line.indexOf("|") !== -1 && i + 1 < lines.length && isTableSep(lines[i + 1])) {
        closeList();
        var t = parseTable(lines, i);
        html += t.html + "\n";
        i = t.next;
        continue;
      }

      var hd = /^(#{1,6})\s+(.*)$/.exec(line);
      if (hd) {
        closeList();
        var lvl = hd[1].length;
        var txt = hd[2].trim();
        var id = slugify(txt.replace(/<[^>]+>/g, ""));
        html += '<h' + lvl + ' id="' + id + '">' + inline(txt) + "</h" + lvl + ">\n";
        i++;
        continue;
      }

      if (/^\s*---+\s*$/.test(line)) {
        closeList();
        html += "<hr>\n";
        i++;
        continue;
      }

      if (/^>\s?/.test(line)) {
        closeList();
        var q = [];
        while (i < lines.length && /^>\s?/.test(lines[i])) { q.push(lines[i].replace(/^>\s?/, "")); i++; }
        html += "<blockquote>" + inline(q.join(" ")) + "</blockquote>\n";
        continue;
      }

      var ul = /^(\s*)[-*+]\s+(.*)$/.exec(line);
      var ol = /^(\s*)\d+\.\s+(.*)$/.exec(line);
      if (ul || ol) {
        var isUl = !!ul;
        var m = isUl ? ul : ol;
        var depth = Math.floor(m[1].length / 2);
        var tag = isUl ? "ul" : "ol";
        if (!inList) {
          html += "<" + tag + ">\n";
          inList = true;
          listType = tag;
          listDepth = depth;
        } else if (depth !== listDepth) {
          // flat nesting support: close/reopen at different depth
          closeList();
          html += "<" + tag + ">\n";
          inList = true;
          listType = tag;
          listDepth = depth;
        }
        html += "<li>" + inline(m[2]) + "</li>\n";
        i++;
        continue;
      }

      if (line.trim() === "") {
        closeList();
        i++;
        continue;
      }

      // paragraph
      closeList();
      var para = [line];
      i++;
      while (
        i < lines.length &&
        lines[i].trim() !== "" &&
        !/^(#{1,6})\s/.test(lines[i]) &&
        !/^```/.test(lines[i]) &&
        !/^\s*---+\s*$/.test(lines[i]) &&
        !/^>\s?/.test(lines[i]) &&
        !/^(\s*)[-*+]\s+/.test(lines[i]) &&
        !/^(\s*)\d+\.\s+/.test(lines[i]) &&
        !(lines[i].indexOf("|") !== -1 && i + 1 < lines.length && isTableSep(lines[i + 1]))
      ) {
        para.push(lines[i]);
        i++;
      }
      html += "<p>" + inline(para.join(" ")) + "</p>\n";
    }
    closeList();
    return html;
  }

  global.mdRender = render;
})(typeof window !== "undefined" ? window : globalThis);
