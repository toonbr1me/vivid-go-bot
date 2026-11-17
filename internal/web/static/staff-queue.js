(() => {
  const target = document.getElementById('staff-queue');
  if (!target || typeof EventSource === 'undefined') return;
  const stream = new EventSource('/admin/tickets/updates');
  let pending = false;
  stream.onmessage = async () => {
    if (pending || document.hidden) return;
    pending = true;
    try {
      const response = await fetch(location.href, { credentials: 'same-origin', cache: 'no-store' });
      if (!response.ok || response.redirected) { stream.close(); return; }
      const page = new DOMParser().parseFromString(await response.text(), 'text/html');
      const next = page.getElementById('staff-queue');
      if (next) target.replaceChildren(...next.childNodes);
    } catch (_) {
      // EventSource retries; the current queue stays usable during an outage.
    } finally { pending = false; }
  };
  addEventListener('pagehide', () => stream.close(), { once: true });
})();
