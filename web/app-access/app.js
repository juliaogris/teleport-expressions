"use strict";

// Two sample scenarios. Each fills both the rule and the input so a visitor can
// evaluate immediately.
const SAMPLES = {
  sample1: {
    rule: [
      'paths: ["/api/v4/projects/{project}/**"]',
      "methods: [GET]",
      'where: contains(user.traits["allowed_projects"], vars.project)',
    ].join("\n"),
    input: [
      "request:",
      "  method: GET",
      "  path: /api/v4/projects/alpha/issues",
      "identity:",
      "  name: alice",
      "  traits:",
      "    allowed_projects: [alpha, beta]",
    ].join("\n"),
  },
  sample2: {
    rule: 'pred: path.match(literal("api", greedy())) && contains(user.roles, "admin")',
    input: [
      "request:",
      "  method: DELETE",
      "  path: /api/anything/here",
      "identity:",
      "  name: bob",
      "  roles: [admin]",
    ].join("\n"),
  },
};

const ruleEl = document.getElementById("rule");
const inputEl = document.getElementById("input");
const resultEl = document.getElementById("result");
const evaluateBtn = document.getElementById("evaluate");

function loadSample(name) {
  ruleEl.value = SAMPLES[name].rule;
  inputEl.value = SAMPLES[name].input;
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

document.getElementById("sample1").addEventListener("click", () => loadSample("sample1"));
document.getElementById("sample2").addEventListener("click", () => loadSample("sample2"));
evaluateBtn.addEventListener("click", render);

// Load and start the WebAssembly module, then enable evaluation.
const go = new Go();
WebAssembly.instantiateStreaming(fetch("eval.wasm"), go.importObject)
  .then((res) => {
    go.run(res.instance);
    loadSample("sample1");
    resultEl.textContent = "Ready. Press Evaluate.";
    evaluateBtn.disabled = false;
  })
  .catch((err) => {
    resultEl.className = "error";
    resultEl.textContent = "Failed to load WebAssembly module: " + err;
  });
