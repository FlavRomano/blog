// Small progressive enhancements for the admin area (CSP forbids inline scripts).
document.addEventListener("click", function (e) {
  var btn = e.target.closest("[data-insert]");
  if (!btn) return;
  var ta = document.getElementById("body");
  if (!ta) return;
  var start = ta.selectionStart;
  var text = btn.getAttribute("data-insert");
  if (start > 0 && ta.value.slice(start - 2, start) !== "\n\n") text = "\n\n" + text;
  text += "\n\n";
  ta.value = ta.value.slice(0, start) + text + ta.value.slice(ta.selectionEnd);
  ta.selectionStart = ta.selectionEnd = start + text.length;
  ta.focus();
  ta.dispatchEvent(new Event("input", { bubbles: true }));
});

document.addEventListener("submit", function (e) {
  var msg = e.target.getAttribute("data-confirm");
  if (msg && !window.confirm(msg)) e.preventDefault();
});
