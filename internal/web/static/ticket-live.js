// Живая переписка: страница подписывается на поток обновлений обращения и
// дописывает новые сообщения сама. Перезагрузка не нужна — ни человеку, ни
// сотруднику.
(function () {
  var chat = document.getElementById('chat');
  if (!chat) return;
  var ticketID = chat.getAttribute('data-ticket');
  if (!ticketID) return;
  var through = 0, acknowledged = 0;
  function markRead() {
    if (chat.getAttribute('data-staff') === 'true' || document.visibilityState !== 'visible' || through <= acknowledged) return;
    var current = through;
    fetch('/support/tickets/' + encodeURIComponent(ticketID) + '/read', {
      method: 'POST', headers: {'Content-Type': 'application/x-www-form-urlencoded'},
      body: 'csrf=' + encodeURIComponent(chat.getAttribute('data-csrf')) + '&through=' + current
    }).then(function (response) { if (response.ok) acknowledged = Math.max(acknowledged, current); }).catch(function () {});
  }
  document.addEventListener('visibilitychange', markRead);

  var seen = {};
  Array.prototype.forEach.call(chat.querySelectorAll('.message'), function (node) {
    seen[node.getAttribute('data-id')] = true;
  });

  function formatTime(value) {
    var date = new Date(value);
    if (isNaN(date.getTime())) return '';
    var pad = function (n) { return (n < 10 ? '0' : '') + n; };
    return pad(date.getDate()) + '.' + pad(date.getMonth() + 1) + '.' + date.getFullYear() +
      ' ' + pad(date.getHours()) + ':' + pad(date.getMinutes());
  }

  function render(messages) {
    var empty = document.getElementById('chat-empty');
    var added = false;
    messages.slice().sort(function(a,b){return a.id-b.id;}).forEach(function (message) {
      through = Math.max(through, Number(message.id) || 0);
      var id = String(message.id);
      if (seen[id]) return;
      seen[id] = true;
      added = true;

      var node = document.createElement('div');
      node.className = 'message' + (message.from_admin ? ' message-support' : '');
      node.setAttribute('data-id', id);

      var head = document.createElement('div');
      head.className = 'message-head';
      var who = document.createElement('strong');
      who.textContent = message.assistant ? 'Помощник' : (message.from_admin ? 'Поддержка' : (chat.getAttribute('data-staff') === 'true' ? 'Клиент' : 'Вы'));
      var when = document.createElement('span');
      when.className = 'muted small';
      when.textContent = formatTime(message.created_at);
      head.appendChild(who);
      head.appendChild(when);

      var text = document.createElement('div');
      text.className = 'message-text';
      text.innerHTML = message.html || '';

      node.appendChild(head);
      node.appendChild(text);
      if (message.attachment) {
        var attachment = document.createElement('a');
        attachment.className = 'btn btn-ghost btn-sm';
        attachment.href = '/support/tickets/' + encodeURIComponent(ticketID) + '/files/' + encodeURIComponent(id);
        attachment.textContent = 'Скачать вложение';
        node.appendChild(attachment);
      }
      chat.appendChild(node);
    });
    if (added && empty) empty.remove();
  }

  var source = new EventSource('/support/tickets/' + ticketID + '/updates');
  source.onmessage = function (event) {
    try {
      var payload = JSON.parse(event.data);
      if (payload && payload.messages) {
        render(payload.messages);
        ['ticket-compose', 'ticket-resolve'].forEach(function(id){var form=document.getElementById(id);if(form)form.hidden=!payload.open;});
        markRead();
      }
    } catch (err) {
      // Поток прислал не то: молча ждём следующего обновления.
    }
  };
})();
