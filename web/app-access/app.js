"use strict";

const ruleEl = document.getElementById("rule");
const inputEl = document.getElementById("input");
const resultEl = document.getElementById("result");
const evaluateBtn = document.getElementById("evaluate");
const topicSelect = document.getElementById("topic-select");
const sampleSelect = document.getElementById("sample-select");
const sugaredCheck = document.getElementById("sugared");

const ruleEditor = CodeEditor.makeEditor(ruleEl, "yaml");
const inputEditor = CodeEditor.makeEditor(inputEl, "yaml");

// topics is a two-level tree: each topic groups a list of examples, and the two
// dropdowns select a topic and then an example within it.
let topics = [];

function topicIndex() {
  return parseInt(topicSelect.value, 10) || 0;
}

function currentExamples() {
  const topic = topics[topicIndex()];
  return topic ? topic.examples : [];
}

// populateExamples fills the example dropdown with the chosen topic's examples.
function populateExamples(t) {
  sampleSelect.innerHTML = "";
  const topic = topics[t];
  if (!topic) return;
  topic.examples.forEach((ex, i) => {
    const opt = document.createElement("option");
    opt.value = String(i);
    opt.textContent = ex.name;
    sampleSelect.appendChild(opt);
  });
}

// loadCurrent loads the selected example into the fields, honoring the sugared
// checkbox. When sugared is unchecked, it asks the WebAssembly module to lower
// the rules to their bare predicate form. The samples are authored sugared, so
// the desugared form is always computed, never hand-written.
function loadCurrent() {
  const s = currentExamples()[sampleSelect.value];
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
// examples does not leave a stale result on screen. It runs only once the module
// is ready, to avoid overwriting the loading message.
function resetResult() {
  if (evaluateBtn.disabled) return;
  resultEl.className = "idle";
  resultEl.textContent = "Ready. Press Evaluate.";
}

// writeHash records the topic, example, and sugared state in the URL hash, so a
// view can be linked or bookmarked. It uses replaceState, so stepping through
// examples does not pile up browser history entries.
function writeHash() {
  const p = new URLSearchParams();
  p.set("topic", topicSelect.value);
  p.set("example", sampleSelect.value);
  p.set("sugared", sugaredCheck.checked ? "1" : "0");
  history.replaceState(null, "", "#" + p.toString());
}

// applyHash restores the topic, example, and sugared state from the URL hash
// when it is present and valid. It runs on load and on hashchange.
function applyHash() {
  const p = new URLSearchParams(location.hash.slice(1));
  const t = parseInt(p.get("topic"), 10);
  if (!Number.isNaN(t) && t >= 0 && t < topics.length) {
    topicSelect.value = String(t);
  }
  populateExamples(topicIndex());
  const e = parseInt(p.get("example"), 10);
  if (!Number.isNaN(e) && e >= 0 && e < currentExamples().length) {
    sampleSelect.value = String(e);
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

// step moves by delta within the current topic. Past the last example it wraps
// to the first example of the next topic, and before the first it wraps to the
// last example of the previous topic, so prev/next walk the whole tree.
function step(delta) {
  if (topics.length === 0) return;
  let t = topicIndex();
  let e = (parseInt(sampleSelect.value, 10) || 0) + delta;
  if (e >= topics[t].examples.length) {
    t = (t + 1) % topics.length;
    e = 0;
  } else if (e < 0) {
    t = (t - 1 + topics.length) % topics.length;
    e = topics[t].examples.length - 1;
  }
  topicSelect.value = String(t);
  populateExamples(t);
  sampleSelect.value = String(e);
  loadCurrent();
}

topicSelect.addEventListener("change", () => {
  populateExamples(topicIndex());
  sampleSelect.value = "0";
  loadCurrent();
});
sampleSelect.addEventListener("change", loadCurrent);
sugaredCheck.addEventListener("change", loadCurrent);
document.getElementById("prev").addEventListener("click", () => step(-1));
document.getElementById("next").addEventListener("click", () => step(1));
evaluateBtn.addEventListener("click", render);

// Cmd/Ctrl+Enter evaluates from anywhere, including while typing in a field.
// Left and right arrows step through the examples, but only when no field is
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

// Restore and track state in the URL hash, including across back and forward
// navigation.
window.addEventListener("hashchange", () => {
  applyHash();
  loadCurrent();
});

// Load the topics, populate the dropdowns, and show the first example.
fetch("samples.json")
  .then((r) => r.json())
  .then((data) => {
    topics = data;
    topicSelect.innerHTML = "";
    topics.forEach((t, i) => {
      const opt = document.createElement("option");
      opt.value = String(i);
      opt.textContent = t.topic;
      topicSelect.appendChild(opt);
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
