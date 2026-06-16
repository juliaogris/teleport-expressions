"use strict";

const exprEl = document.getElementById("expr");
const inputEl = document.getElementById("input");
const resultEl = document.getElementById("result");
const evaluateBtn = document.getElementById("evaluate");
const sampleSelect = document.getElementById("sample-select");

const exprEditor = CodeEditor.makeEditor(exprEl, "expr");
const inputEditor = CodeEditor.makeEditor(inputEl, "yaml");

let samples = [];

function loadSample(index) {
  const s = samples[index];
  if (!s) return;
  exprEl.value = s.expr;
  inputEl.value = s.input;
  exprEditor.update();
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

// writeHash records the selected sample in the URL hash, so a view can be
// linked or bookmarked. It uses replaceState, so stepping through samples does
// not pile up browser history entries.
function writeHash() {
  history.replaceState(null, "", "#sample=" + sampleSelect.value);
}

// applyHash restores the selected sample from the URL hash when it is present
// and valid. It runs on load and on hashchange.
function applyHash() {
  const p = new URLSearchParams(location.hash.slice(1));
  const idx = parseInt(p.get("sample"), 10);
  if (!Number.isNaN(idx) && idx >= 0 && idx < samples.length) {
    sampleSelect.value = String(idx);
  }
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
  // evaluateLabelExpression is registered by the Go WebAssembly module.
  const out = evaluateLabelExpression(exprEl.value, inputEl.value);
  resultEl.className = "";
  if (out.error) {
    resultEl.classList.add("error");
    resultEl.innerHTML =
      '<span class="verdict error">error</span>\n' + escapeHtml(out.error);
    return;
  }
  const cls = out.match ? "match" : "nomatch";
  resultEl.classList.add(cls);
  resultEl.innerHTML =
    '<span class="verdict ' +
    cls +
    '">' +
    (out.match ? "match: true" : "match: false") +
    "</span>";
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
  loadSample(i);
}

sampleSelect.addEventListener("change", () => loadSample(sampleSelect.value));
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
  loadSample(sampleSelect.value);
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
    loadSample(sampleSelect.value);
  });

// Load and start the WebAssembly module, then enable evaluation.
const go = new Go();
WebAssembly.instantiateStreaming(fetch("eval.wasm"), go.importObject)
  .then((res) => {
    go.run(res.instance);
    resultEl.className = "idle";
    resultEl.textContent = "Ready. Press Evaluate.";
    evaluateBtn.disabled = false;
  })
  .catch((err) => {
    resultEl.className = "error";
    resultEl.textContent = "Failed to load WebAssembly module: " + err;
  });
