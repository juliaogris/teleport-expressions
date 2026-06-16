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
}

function render() {
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
    text += "\ncaptures:";
    for (const k of keys) {
      text += "\n  " + escapeHtml(k) + ": " + escapeHtml(vars[k]);
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

sampleSelect.addEventListener("change", loadCurrent);
sugaredCheck.addEventListener("change", loadCurrent);
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
    loadCurrent();
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
