"use strict";

// Two sample scenarios. Each fills both the expression and the YAML input so a
// visitor can evaluate immediately.
const SAMPLES = {
  sample1: {
    expr: 'labels["env"] == "prod" && contains(user.spec.traits["groups"], labels["owner"])',
    input: [
      "labels:",
      "  env: prod",
      "  owner: devs",
      "username: alice@example.com",
      "traits:",
      "  groups: [devs, security]",
    ].join("\n"),
  },
  sample2: {
    expr: 'regexp.match(email.local(set(user.metadata.name)), "a.*") && contains(labels_matching("team-*"), "platform")',
    input: [
      "labels:",
      "  team-a: platform",
      "  region: us-west-2",
      "username: anna@example.com",
      "traits: {}",
    ].join("\n"),
  },
};

const exprEl = document.getElementById("expr");
const inputEl = document.getElementById("input");
const resultEl = document.getElementById("result");
const evaluateBtn = document.getElementById("evaluate");

function loadSample(name) {
  exprEl.value = SAMPLES[name].expr;
  inputEl.value = SAMPLES[name].input;
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
  return s
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
