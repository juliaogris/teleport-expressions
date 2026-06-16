"use strict";

const exprEl = document.getElementById("expr");
const inputEl = document.getElementById("input");
const resultEl = document.getElementById("result");
const evaluateBtn = document.getElementById("evaluate");
const sampleSelect = document.getElementById("sample-select");

const shareBtn = document.getElementById("share");
const shareDialog = document.getElementById("share-dialog");
const shareUrlEl = document.getElementById("share-url");
const shareNameEl = document.getElementById("share-name");
const shareCopyEl = document.getElementById("share-copy");

// Set the loading message from JS rather than in the HTML, so the result box
// has no leading or trailing whitespace under its pre-wrap white-space, which
// would otherwise render as extra spacing the later messages do not have.
resultEl.textContent = "Loading WebAssembly module…";

const exprEditor = CodeEditor.makeEditor(exprEl, "expr");
const inputEditor = CodeEditor.makeEditor(inputEl, "yaml");

let samples = [];

// sharedMode is true while a #content= link is open. The sample picker then
// shows a synthetic entry with the snippet's name; picking a real sample or
// stepping leaves shared mode. sharedName defaults to "untitled".
let sharedMode = false;
let sharedName = "";

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

// buildSampleOptions fills the sample dropdown with the real samples. It is
// also used to rebuild the dropdown after leaving shared mode.
function buildSampleOptions() {
  sampleSelect.innerHTML = "";
  samples.forEach((s, i) => {
    const opt = document.createElement("option");
    opt.value = String(i);
    opt.textContent = s.name;
    sampleSelect.appendChild(opt);
  });
}

// removeSharedOption drops the synthetic "shared" entry, leaving the user's
// freshly chosen real sample selected.
function removeSharedOption() {
  const opt = sampleSelect.querySelector('option[value="shared"]');
  if (opt) opt.remove();
}

// isModified reports whether the editors differ from the given sample.
function isModified(s) {
  return exprEl.value !== s.expr || inputEl.value !== s.input;
}

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

// applyHashAndLoad restores the view from the URL hash and loads it. A
// #content= snippet wins over #sample and opens in shared mode; otherwise the
// selected sample is restored and loaded. It runs on load and on hashchange.
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
  buildSampleOptions();
  const idx = parseInt(p.get("sample"), 10);
  if (!Number.isNaN(idx) && idx >= 0 && idx < samples.length) {
    sampleSelect.value = String(idx);
  }
  loadSample(sampleSelect.value);
}

// enterSharedMode opens a shared snippet: it prepends a synthetic entry showing
// the snippet name and fills the editors from the snippet. The picker stays a
// real dropdown, so picking a real sample exits.
function enterSharedMode(snip) {
  sharedMode = true;
  sharedName = snip.name ? snip.name : "untitled";
  buildSampleOptions();
  const opt = document.createElement("option");
  opt.value = "shared";
  opt.textContent = sharedName;
  sampleSelect.insertBefore(opt, sampleSelect.firstChild);
  sampleSelect.value = "shared";
  exprEl.value = snip.expr || "";
  inputEl.value = snip.input || "";
  exprEditor.update();
  inputEditor.update();
  resetResult();
}

// contentHash encodes the current editor state as a #content= snippet.
function contentHash(name) {
  return (
    "#content=" +
    b64urlEncode({
      name: name || "",
      expr: exprEl.value,
      input: inputEl.value,
    })
  );
}

// computeShareHash returns the hash for the current view. An unmodified sample
// whose name is still the sample's own keeps the short #sample form. Editing
// the expression or input, giving the snippet a different name, or sharing an
// already-shared snippet becomes a #content= link, since the short form has
// nowhere to carry a custom name.
function computeShareHash(name) {
  if (!sharedMode) {
    const s = samples[sampleSelect.value];
    if (s && !isModified(s) && name === s.name) {
      return "#sample=" + sampleSelect.value;
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
// snippet name in shared mode, or the sample name while the sample is
// unmodified. Once the sample is edited the name no longer describes the
// content, so leave the field empty and let the "untitled" placeholder show.
function openShare() {
  if (sharedMode) {
    shareNameEl.value = sharedName;
  } else {
    const s = samples[sampleSelect.value];
    shareNameEl.value = s && !isModified(s) ? s.name : "";
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
  if (sharedMode) {
    // Stepping leaves shared mode and lands on the first or last sample,
    // from where further steps wrap as usual.
    sharedMode = false;
    removeSharedOption();
    const i = delta > 0 ? 0 : n - 1;
    sampleSelect.value = String(i);
    loadSample(i);
    return;
  }
  const i = ((parseInt(sampleSelect.value, 10) || 0) + delta + n) % n;
  sampleSelect.value = String(i);
  loadSample(i);
}

sampleSelect.addEventListener("change", () => {
  if (sharedMode && sampleSelect.value !== "shared") {
    // Picking a real sample leaves shared mode; the chosen option stays
    // selected and only the synthetic entry is removed.
    sharedMode = false;
    removeSharedOption();
  }
  loadSample(sampleSelect.value);
});
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
window.addEventListener("hashchange", applyHashAndLoad);

// Load the samples, populate the dropdown, and show the first one.
fetch("samples.json")
  .then((r) => r.json())
  .then((data) => {
    samples = data;
    buildSampleOptions();
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
  })
  .catch((err) => {
    resultEl.className = "error";
    resultEl.textContent = "Failed to load WebAssembly module: " + err;
  });
