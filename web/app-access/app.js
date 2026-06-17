"use strict";

const ruleEl = document.getElementById("rule");
const inputEl = document.getElementById("input");
const resultEl = document.getElementById("result");
const evaluateBtn = document.getElementById("evaluate");
const topicSelect = document.getElementById("topic-select");
const sampleSelect = document.getElementById("sample-select");
const sugaredCheck = document.getElementById("sugared");

// Set the loading message from JS rather than in the HTML, so the result box
// has no leading or trailing whitespace under its pre-wrap white-space, which
// would otherwise render as extra spacing the later messages do not have.
resultEl.textContent = "Loading WebAssembly module…";

const ruleEditor = CodeEditor.makeEditor(ruleEl, "yaml");
const inputEditor = CodeEditor.makeEditor(inputEl, "yaml");

const shareBtn = document.getElementById("share");
const shareDialog = document.getElementById("share-dialog");
const shareUrlEl = document.getElementById("share-url");
const shareNameEl = document.getElementById("share-name");
const shareCopyEl = document.getElementById("share-copy");

// topics is a two-level tree: each topic groups a list of examples, and the two
// dropdowns select a topic and then an example within it.
let topics = [];

// sharedMode is true while a #content= link is open. The pickers then show a
// synthetic "shared" topic and the snippet's name, and editing is free-form;
// picking a real topic or example, or stepping, leaves shared mode. sharedName
// is the snippet's name, defaulting to "untitled".
let sharedMode = false;
let sharedName = "";
// sharedSnippet holds the shared rule, input, and the sugared state the rule
// was captured in, so the sugared toggle can re-derive the rule in shared mode.
let sharedSnippet = null;

function topicIndex() {
  return parseInt(topicSelect.value, 10) || 0;
}

// b64urlEncode serializes an object to JSON and encodes it as base64url (the
// "-_" alphabet, no padding) over its UTF-8 bytes. Plain btoa is wrong here: it
// fails on non-ASCII text, and "+/=" are not URL-safe.
function b64urlEncode(obj) {
  const bytes = new TextEncoder().encode(JSON.stringify(obj));
  let bin = "";
  for (const b of bytes) bin += String.fromCharCode(b);
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

// b64urlDecode reverses b64urlEncode, returning the parsed object, or null when
// the string is not a valid encoded snippet.
function b64urlDecode(s) {
  try {
    const b64 = s.replace(/-/g, "+").replace(/_/g, "/");
    const bin = atob(b64);
    const bytes = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
    return JSON.parse(new TextDecoder().decode(bytes));
  } catch (e) {
    return null;
  }
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

// buildTopicOptions fills the topic dropdown with the real topics, one option
// per topic. It is also used to rebuild the dropdown after leaving shared mode.
function buildTopicOptions() {
  topicSelect.innerHTML = "";
  topics.forEach((t, i) => {
    const opt = document.createElement("option");
    opt.value = String(i);
    opt.textContent = t.topic;
    topicSelect.appendChild(opt);
  });
}

// prependOption inserts a synthetic option at the top of a select.
function prependOption(select, value, text) {
  const opt = document.createElement("option");
  opt.value = value;
  opt.textContent = text;
  select.insertBefore(opt, select.firstChild);
}

// removeSharedOptions drops the synthetic "shared" entries from both pickers.
// It leaves the user's freshly chosen real option selected.
function removeSharedOptions() {
  for (const sel of [topicSelect, sampleSelect]) {
    const opt = sel.querySelector('option[value="shared"]');
    if (opt) opt.remove();
  }
}

// canonicalRuleFor returns the rule text the given example renders to under the
// current sugared state, mirroring loadCurrent so the modified check compares
// against exactly what was loaded.
function canonicalRuleFor(ex) {
  if (sugaredCheck.checked) return ex.rule;
  if (typeof desugarAppResources === "function") {
    const out = desugarAppResources(ex.rule);
    if (out && !out.error) return out.yaml;
  }
  return ex.rule;
}

// isModified reports whether the editors differ from what the given example
// plus the current sugared state renders. Toggling sugared alone is not a
// modification, since canonicalRuleFor tracks the sugared state.
function isModified(ex) {
  return ruleEl.value !== canonicalRuleFor(ex) || inputEl.value !== ex.input;
}

// loadCurrent loads the selected example into the fields, honoring the sugared
// checkbox. When sugared is unchecked, it asks the WebAssembly module to lower
// the rules to their bare predicate form. The samples are authored sugared, so
// the desugared form is always computed, never hand-written.
function loadCurrent() {
  if (sharedMode) {
    applySharedRule();
    return;
  }
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

// applyHashAndLoad restores the view from the URL hash and loads it. A
// #content= snippet wins over #topic/#example and opens in shared mode;
// otherwise the topic, example, and sugared state are restored and the canned
// example is loaded. It runs on load and on hashchange.
function applyHashAndLoad() {
  const p = new URLSearchParams(location.hash.slice(1));
  const content = p.get("content");
  if (content) {
    const snip = b64urlDecode(content);
    if (snip) {
      enterSharedMode(snip);
      return;
    }
  }
  sharedMode = false;
  buildTopicOptions();
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
  loadCurrent();
}

// enterSharedMode opens a shared snippet: it prepends a synthetic "shared"
// topic and a name entry, fills the editors from the snippet, and leaves the
// pickers as real dropdowns so picking a real topic or example exits. The
// example slot lists topic 0's examples beneath the name, so a real example is
// always pickable.
function enterSharedMode(snip) {
  sharedMode = true;
  sharedName = snip.name ? snip.name : "untitled";
  sharedSnippet = {
    rule: snip.rule || "",
    input: snip.input || "",
    sugared: !!snip.sugared,
  };
  buildTopicOptions();
  prependOption(topicSelect, "shared", "shared");
  topicSelect.value = "shared";
  populateExamples(0);
  prependOption(sampleSelect, "shared", sharedName);
  sampleSelect.value = "shared";
  sugaredCheck.checked = sharedSnippet.sugared;
  inputEl.value = sharedSnippet.input;
  inputEditor.update();
  applySharedRule();
}

// applySharedRule sets the rule editor to the shared snippet's rule in the form
// the sugared toggle asks for. The snippet stores its rule in
// sharedSnippet.sugared form, so the bare form is derived by desugaring on
// demand when the sugared source is held. Re-sugaring is not possible, so a
// snippet shared in desugared form stays desugared. The input is left as the
// snippet's, so toggling sugared does not disturb an edited input.
function applySharedRule() {
  let ruleText = sharedSnippet.rule;
  if (
    !sugaredCheck.checked &&
    sharedSnippet.sugared &&
    typeof desugarAppResources === "function"
  ) {
    const out = desugarAppResources(sharedSnippet.rule);
    if (out && !out.error) {
      ruleText = out.yaml;
    }
  }
  ruleEl.value = ruleText;
  ruleEditor.update();
  resetResult();
}

// contentHash encodes the current editor state as a #content= snippet.
function contentHash(name) {
  return (
    "#content=" +
    b64urlEncode({
      name: name || "",
      rule: ruleEl.value,
      input: inputEl.value,
      sugared: sugaredCheck.checked,
    })
  );
}

// computeShareHash returns the hash for the current view. An unmodified example
// whose name is still the example's own keeps the short, readable
// #topic/#example form. Editing the rule or input, giving the snippet a
// different name, or sharing an already-shared snippet becomes a #content=
// link, since the short form has nowhere to carry a custom name.
function computeShareHash(name) {
  if (!sharedMode) {
    const ex = currentExamples()[sampleSelect.value];
    if (ex && !isModified(ex) && name === ex.name) {
      const p = new URLSearchParams();
      p.set("topic", topicSelect.value);
      p.set("example", sampleSelect.value);
      p.set("sugared", sugaredCheck.checked ? "1" : "0");
      return "#" + p.toString();
    }
  }
  return contentHash(name);
}

// updateShareUrl recomputes the link shown in the dialog from the current
// editors and the name field, so the URL reflects the name as it is typed.
function updateShareUrl() {
  const hash = computeShareHash(shareNameEl.value.trim());
  shareUrlEl.value = location.origin + location.pathname + hash;
}

// openShare fills and shows the share dialog. The name field is seeded with the
// snippet name in shared mode, or the example name while the example is
// unmodified. Once the example is edited the name no longer describes the
// content, so leave the field empty and let the "untitled" placeholder show.
function openShare() {
  if (sharedMode) {
    shareNameEl.value = sharedName;
  } else {
    const ex = currentExamples()[sampleSelect.value];
    shareNameEl.value = ex && !isModified(ex) ? ex.name : "";
  }
  updateShareUrl();
  shareDialog.showModal();
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
  if (!out.allowed && out.denyKind) {
    text += "\ndeny_kind: " + escapeHtml(out.denyKind);
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
  if (sharedMode) {
    // Stepping leaves shared mode and lands on the first canned example, from
    // where further steps walk the tree as usual.
    sharedMode = false;
    removeSharedOptions();
    topicSelect.value = "0";
    populateExamples(0);
    sampleSelect.value = "0";
    loadCurrent();
    return;
  }
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
  // Picking a real topic leaves shared mode. removeSharedOptions drops the
  // synthetic topic entry; populateExamples then rebuilds the example list.
  sharedMode = false;
  removeSharedOptions();
  populateExamples(topicIndex());
  sampleSelect.value = "0";
  loadCurrent();
});
sampleSelect.addEventListener("change", () => {
  if (sharedMode) {
    // Picking one of topic 0's real examples leaves shared mode. The chosen
    // option stays selected; only the synthetic entries are removed.
    sharedMode = false;
    removeSharedOptions();
    topicSelect.value = "0";
  }
  loadCurrent();
});
sugaredCheck.addEventListener("change", loadCurrent);
document.getElementById("prev").addEventListener("click", () => step(-1));
document.getElementById("next").addEventListener("click", () => step(1));
evaluateBtn.addEventListener("click", render);

shareBtn.addEventListener("click", openShare);
shareNameEl.addEventListener("input", updateShareUrl);
shareDialog.addEventListener("click", (e) => {
  // Close when the backdrop (the dialog element itself) is clicked.
  if (e.target === shareDialog) shareDialog.close();
});
const copyLabel = shareCopyEl.querySelector(".copy-label");
shareCopyEl.addEventListener("click", () => {
  navigator.clipboard.writeText(shareUrlEl.value).then(() => {
    // Confirm with a checkmark and "Copied", then close the dialog. Reset the
    // button so the next open shows "Copy" again.
    shareCopyEl.classList.add("copied");
    copyLabel.textContent = "Copied";
    setTimeout(() => {
      shareDialog.close();
      shareCopyEl.classList.remove("copied");
      copyLabel.textContent = "Copy";
    }, 1000);
  });
});

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
window.addEventListener("hashchange", applyHashAndLoad);

// Load the topics, populate the dropdowns, and show the first example.
fetch("samples.json")
  .then((r) => r.json())
  .then((data) => {
    topics = data;
    buildTopicOptions();
    applyHashAndLoad();
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
