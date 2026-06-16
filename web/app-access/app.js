"use strict";

const ruleEl = document.getElementById("rule");
const inputEl = document.getElementById("input");
const resultEl = document.getElementById("result");
const evaluateBtn = document.getElementById("evaluate");
const sampleSelect = document.getElementById("sample-select");
const sugaredCheck = document.getElementById("sugared");

const ruleEditor = CodeEditor.makeEditor(ruleEl, "yaml");
const inputEditor = CodeEditor.makeEditor(inputEl, "yaml");

let samples = [];

// loadCurrent loads the selected sample into the fields, honoring the sugared
// checkbox. When sugared is unchecked, it asks the WebAssembly module to lower
// the rules to their bare predicate form. The samples are authored sugared, so
// the desugared form is always computed, never hand-written.
function loadCurrent() {
  const s = samples[sampleSelect.value];
  if (!s) return;
  let ruleText = s.rule;
  if (!sugaredCheck.checked && typeof desugarAppResources === "function") {
    const out = desugarAppResources(s.rule);
    if (out && !out.error) {
      ruleText = out.yaml;
    }
  }
  ruleEl.value = ruleText;
  inputEl.value = s.input;
  ruleEditor.update();
  inputEditor.update();
  resetResult();
  writeHash();
}

// resetResult clears any prior verdict back to the idle prompt, so switching
// samples does not leave a stale result on screen. It runs only once the module
// is ready, to avoid overwriting the loading message.
function resetResult() {
  if (evaluateBtn.disabled) return;
  resultEl.className = "idle";
  resultEl.textContent = "Ready. Press Evaluate.";
}

// writeHash records the selected sample and sugared state in the URL hash, so a
// view can be linked or bookmarked. It uses replaceState, so stepping through
// samples does not pile up browser history entries.
function writeHash() {
  const p = new URLSearchParams();
  p.set("sample", sampleSelect.value);
  p.set("sugared", sugaredCheck.checked ? "1" : "0");
  history.replaceState(null, "", "#" + p.toString());
}

// applyHash restores the sample and sugared state from the URL hash when it is
// present and valid. It runs on load and on hashchange.
function applyHash() {
  const p = new URLSearchParams(location.hash.slice(1));
  const idx = parseInt(p.get("sample"), 10);
  if (!Number.isNaN(idx) && idx >= 0 && idx < samples.length) {
    sampleSelect.value = String(idx);
  }
  const sugared = p.get("sugared");
  if (sugared === "0") sugaredCheck.checked = false;
  else if (sugared === "1") sugaredCheck.checked = true;
}

// render shows a brief "Evaluating…" state, then runs the evaluation. The short
// delay makes it visible that an evaluation happened, which matters most when it
// is triggered by the keyboard rather than a button press.
function render() {
  resultEl.className = "idle";
  resultEl.textContent = "Evaluating…";
  setTimeout(renderNow, 250);
}

function renderNow() {
  // evaluateAppAccessRule is registered by the Go WebAssembly module.
  const out = evaluateAppAccessRule(ruleEl.value, inputEl.value);
  resultEl.className = "";
  if (out.error) {
    resultEl.classList.add("error");
    resultEl.innerHTML =
      '<span class="verdict error">error</span>\n' + escapeHtml(out.error);
    return;
  }
  const cls = out.allowed ? "match" : "nomatch";
  resultEl.classList.add(cls);
  let text =
    '<span class="verdict ' +
    cls +
    '">' +
    (out.allowed ? "allowed: true" : "allowed: false") +
    "</span>";
  const vars = out.vars || {};
  const keys = Object.keys(vars);
  if (out.allowed && keys.length > 0) {
    text += "\nvars:";
    for (const k of keys) {
      text += "\n  " + escapeHtml(k) + ": " + escapeHtml(vars[k]);
    }
  }
  if (out.allowed && out.allowCode) {
    text += "\nallow_code: " + escapeHtml(out.allowCode);
    if (out.allowReason) {
      text += "\nallow_reason: " + escapeHtml(out.allowReason);
    }
  }
  const hints = out.denyHints || [];
  if (!out.allowed && hints.length > 0) {
    text += "\ndeny_hints:";
    for (const h of hints) {
      text += "\n  - deny_code: " + escapeHtml(h.denyCode);
      if (h.denyReason) {
        text += "\n    deny_reason: " + escapeHtml(h.denyReason);
      }
    }
  }
  resultEl.innerHTML = text;
}

function escapeHtml(s) {
  return String(s)
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;");
}

function step(delta) {
  const n = samples.length;
  if (!n) return;
  const i = ((parseInt(sampleSelect.value, 10) || 0) + delta + n) % n;
  sampleSelect.value = String(i);
  loadCurrent();
}

sampleSelect.addEventListener("change", loadCurrent);
sugaredCheck.addEventListener("change", loadCurrent);
document.getElementById("prev").addEventListener("click", () => step(-1));
document.getElementById("next").addEventListener("click", () => step(1));
evaluateBtn.addEventListener("click", render);

// Cmd/Ctrl+Enter evaluates from anywhere, including while typing in a field.
// Left and right arrows step through the samples, but only when no field is
// focused, so they do not fight cursor movement while editing.
document.addEventListener("keydown", (e) => {
  if ((e.metaKey || e.ctrlKey) && e.key === "Enter" && !evaluateBtn.disabled) {
    e.preventDefault();
    render();
    return;
  }
  if (e.metaKey || e.ctrlKey || e.altKey) return;
  const tag = e.target && e.target.tagName;
  if (tag === "TEXTAREA" || tag === "INPUT" || tag === "SELECT") return;
  if (e.key === "ArrowLeft") {
    e.preventDefault();
    step(-1);
  } else if (e.key === "ArrowRight") {
    e.preventDefault();
    step(1);
  }
});

// Restore and track sample state in the URL hash, including across back and
// forward navigation.
window.addEventListener("hashchange", () => {
  applyHash();
  loadCurrent();
});

// Load the samples, populate the dropdown, and show the first one.
fetch("samples.json")
  .then((r) => r.json())
  .then((data) => {
    samples = data;
    sampleSelect.innerHTML = "";
    samples.forEach((s, i) => {
      const opt = document.createElement("option");
      opt.value = String(i);
      opt.textContent = s.name;
      sampleSelect.appendChild(opt);
    });
    applyHash();
    loadCurrent();
  });

// Load and start the WebAssembly module, then enable evaluation.
const go = new Go();
WebAssembly.instantiateStreaming(fetch("eval.wasm"), go.importObject)
  .then((res) => {
    go.run(res.instance);
    resultEl.className = "idle";
    resultEl.textContent = "Ready. Press Evaluate.";
    evaluateBtn.disabled = false;
    // Re-render now that desugarAppResources is registered, so a desugared
    // view requested before the module loaded is filled in.
    loadCurrent();
  })
  .catch((err) => {
    resultEl.className = "error";
    resultEl.textContent = "Failed to load WebAssembly module: " + err;
  });
