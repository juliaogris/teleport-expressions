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
}

function render() {
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

sampleSelect.addEventListener("change", () => loadSample(sampleSelect.value));
evaluateBtn.addEventListener("click", render);

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
    loadSample(0);
  });

// Load and start the WebAssembly module, then enable evaluation.
const go = new Go();
WebAssembly.instantiateStreaming(fetch("eval.wasm"), go.importObject)
  .then((res) => {
    go.run(res.instance);
    resultEl.textContent = "Ready. Press Evaluate.";
    evaluateBtn.disabled = false;
  })
  .catch((err) => {
    resultEl.className = "error";
    resultEl.textContent = "Failed to load WebAssembly module: " + err;
  });
