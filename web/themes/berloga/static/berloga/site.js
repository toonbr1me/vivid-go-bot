"use strict";

document.addEventListener("click", async (event) => {
  const button = event.target.closest("[data-copy-target]");
  if (!button) return;
  const input = document.getElementById(button.dataset.copyTarget);
  if (!input) return;
  input.select();
  try {
    await navigator.clipboard.writeText(input.value);
    button.textContent = "Скопировано";
  } catch {
    button.textContent = "Скопируйте выделенную ссылку";
  }
});

function revealHash() {
  const target = document.getElementById(location.hash.slice(1));
  if (!target) return;
  const disclosure = target.closest("details");
  if (disclosure) disclosure.open = true;
  target.scrollIntoView({block: "start"});
}
window.addEventListener("hashchange", revealHash);
if (location.hash) revealHash();
