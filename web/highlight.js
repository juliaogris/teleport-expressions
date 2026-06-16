"use strict";

// A dependency-free syntax highlighter for the input fields. A textarea cannot
// be colored directly, so each editor layers a transparent textarea over a
// <pre> that mirrors its text with colored token spans. The textarea keeps the
// caret and editing; the <pre> behind it shows the colors. The two share an
// identical box model so the characters line up exactly.
window.CodeEditor = (function () {
  function escapeHtml(s) {
    return s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  }

  function span(cls, text) {
    return '<span class="t-' + cls + '">' + escapeHtml(text) + "</span>";
  }

  // tokenize walks src with a global regex, escaping the gaps between matches
  // and handing each match to render, which returns the HTML for it.
  function tokenize(src, re, render) {
    let out = "";
    let last = 0;
    let m;
    re.lastIndex = 0;
    while ((m = re.exec(src)) !== null) {
      if (m.index > last) {
        out += escapeHtml(src.slice(last, m.index));
      }
      out += render(m);
      last = m.index + m[0].length;
      if (m[0].length === 0) {
        re.lastIndex++;
      }
    }
    out += escapeHtml(src.slice(last));
    return out;
  }

  // Predicate / label expression grammar.
  const EXPR_RE =
    /("(?:[^"\\]|\\.)*")|(\b\d+\b)|(\b(?:true|false)\b)|([A-Za-z_][\w.]*)(?=\s*\()|([A-Za-z_][\w.]*)|(&&|\|\||==|!=|<=|>=|[<>!])|([()[\],])/g;

  function highlightExpr(src) {
    return tokenize(src, EXPR_RE, function (m) {
      if (m[1]) return span("str", m[1]);
      if (m[2]) return span("num", m[2]);
      if (m[3]) return span("bool", m[3]);
      if (m[4]) return span("fn", m[4]);
      if (m[5]) return span("ident", m[5]);
      if (m[6]) return span("op", m[6]);
      if (m[7]) return span("punct", m[7]);
      return escapeHtml(m[0]);
    });
  }

  // YAML grammar. Keys are matched only at the start of a line (after optional
  // indent and a list dash), so values that look like keys are left alone.
  const YAML_RE =
    /(#[^\n]*)|("(?:[^"\\]|\\.)*"|'[^']*')|(^[ \t]*(?:- )?)([\w.\-/]+)(:)|\b(true|false|null)\b|(\b\d+(?:\.\d+)?\b)|([[\]{}])/gm;

  function highlightYaml(src) {
    return tokenize(src, YAML_RE, function (m) {
      if (m[1]) return span("comment", m[1]);
      if (m[2]) return span("str", m[2]);
      if (m[4] !== undefined) {
        return escapeHtml(m[3]) + span("key", m[4]) + span("punct", m[5]);
      }
      if (m[6]) return span("bool", m[6]);
      if (m[7]) return span("num", m[7]);
      if (m[8]) return span("punct", m[8]);
      return escapeHtml(m[0]);
    });
  }

  const LANGS = { expr: highlightExpr, yaml: highlightYaml };

  // makeEditor wraps an existing textarea in the highlight overlay and returns
  // an object with an update method to re-render after the value changes
  // programmatically.
  function makeEditor(textarea, lang) {
    const highlight = LANGS[lang] || escapeHtml;

    const wrap = document.createElement("div");
    wrap.className = "editor";
    const pre = document.createElement("pre");
    pre.className = "editor-hl";
    pre.setAttribute("aria-hidden", "true");
    const code = document.createElement("code");
    pre.appendChild(code);

    textarea.parentNode.insertBefore(wrap, textarea);
    wrap.appendChild(pre);
    wrap.appendChild(textarea);

    function update() {
      const value = textarea.value;
      // A trailing newline needs a trailing space so the last line keeps its
      // height in the <pre>.
      code.innerHTML = highlight(value) + (value.endsWith("\n") ? " " : "");
    }

    function syncScroll() {
      pre.scrollTop = textarea.scrollTop;
      pre.scrollLeft = textarea.scrollLeft;
    }

    textarea.addEventListener("input", update);
    textarea.addEventListener("scroll", syncScroll);
    update();

    return { update: update };
  }

  return { makeEditor: makeEditor };
})();
