# Темы лендинга и кабинета

Тема — каталог, заданный `WEB_DIR`. Рабочий пример: `web/themes/starter`.
Скопируйте его в свой каталог и задайте `WEB_DIR` при запуске. В Docker
каталог темы должен быть смонтирован внутрь контейнера, например в `/app/web`.
После изменений нужен перезапуск. Неуказанные настройки и шаблоны берутся
из встроенных ресурсов. Бизнес-операции и авторизация остаются в Go.

```text
my-theme/
  theme.json
  bot.json                         # необязательные тексты Telegram
  knowledge.md                     # необязательная база знаний
  static/brand.css
  static/app.js
  templates/components/*.html       # общие именованные компоненты, вложенные папки разрешены
  templates/layouts/app.html        # отдельный каркас кабинета
  templates/pages/about.html        # дополнительная страница
```

## Настройки темы

В `theme.json` доступны прежние `brand`, `colors`, `blocks`, FAQ и тексты,
а также:

- `locale` — язык HTML; `logo_url` — URL логотипа.
- `navigation` — массив `{label, href}` ссылок основной навигации;
  пустой массив убирает ссылки.
- `stylesheets` и `scripts` — дополнительные CSS и JS. Используйте
  `/static/...`: политика безопасности разрешает скрипты своего сервера,
  произвольные внешние CDN не разрешены. Не используйте inline JS.
- `texts` — словарь подписей для `{{text "ключ" "запасная подпись"}}`.
  В базовом каркасе есть `nav.account`, `nav.login`, `nav.logout`, `footer.caption`.
- `data` — произвольные JSON-данные, доступные как `.Site.Data`.
- `pages` — настройки конкретных страниц.

Бренд и фактические контакты из настроек приложения приоритетнее темы.

## Страницы и каркасы

```json
{"pages": {
  "landing": {"title": "Главная", "description": "Описание для поиска"},
  "account": {
    "layout": "layouts/app.html",
    "stylesheets": ["/static/app.css"],
    "scripts": ["/static/app.js"],
    "texts": {"nav.logout": "Завершить сеанс"},
    "data": {"help_label": "Помощь с подключением"}
  },
  "about": {
    "path": "/about",
    "template": "pages/about.html",
    "title": "О проекте",
    "require_login": false,
    "data": {"heading": "Наш проект"}
  }
}}
```

Ключ встроенной страницы совпадает с именем её файла без `.html`:
`landing`, `buy`, `login`, `account`, `connect`, `support`, `ticket`,
`documents`, `pay_result`, `notice`, `partner`, `admin` и `admin_*`.
`template` заменяет содержимое страницы, `layout` — каркас. Пути задаются
относительно `WEB_DIR/templates`; выход за этот каталог запрещён.
Сохраняется и прежнее переопределение через `templates/account.html`.

Новые страницы требуют уникальный `path`. Служебные адреса и их поддеревья
зарезервированы. `require_login: true` использует существующий вход сервиса.
Это пользовательские страницы, не способ создавать новые API или админские
разрешения. Маршруты приложения не заменяются темой.

Каркас определяет `{{define "layout"}}...{{template "content" .}}...{{end}}`,
страница — `{{define "content"}}...{{end}}`. Встроенный каркас дополнительно
позволяет переопределять `head`, `header`, `footer`, `scripts`.
Для POST-форм сохраняйте существующие action/имена полей и скрытое поле
`<input type="hidden" name="csrf" value="{{.CSRF}}">`.

В каждой странице доступны `.Site`, `.Meta`, `.Subject`, `.CSRF`, `.BotURL`,
`.SiteURL`, `.Path`, `.Now`, `.Year`, `.View`, `.Page`, `.Data`.
`.Data` содержит типизированные данные обработчика; `.Page.Data` — JSON-данные
темы. Пользовательские страницы получают свои данные также в `.Data`.
Список экранных данных смотрите в `internal/web/pages.go`, `account.go`,
`support.go`, `partner.go`, `admin.go`.

## Общие компоненты и произвольные блоки

Все `templates/components/**/*.html` автоматически загружаются в каждую
страницу. Используйте уникальные имена, например `brand/intro`.

```html
{{define "brand/intro"}}
<section id="{{.Block.ID}}" class="{{.Block.Class}}">
  <h1>{{.Block.Title}}</h1>
  <p>{{.Block.Text}}</p>
  <p>{{.Page.Site.Data.audience}}</p>
</section>
{{end}}
```

```json
{"blocks": [
  {"template": "brand/intro", "id": "intro", "title": "Ваш продукт", "text": "Ваш текст"},
  {"type": "plans", "title": "Тарифы"}
]}
```

Блок с `template` получает `{Block, Page}`. Кроме прежних полей у блока есть
`id`, `class`, `image_url`, `image_alt`, `data`. Их использование определяет
сам компонент. `pages.landing.blocks` заменяет блоки только главной страницы;
другие шаблоны тоже могут обходить `.Page.Blocks` самостоятельно.

Из любого шаблона можно вызвать
`{{include "brand/intro" (dict "Block" . "Page" $)}}`.
`include` выполняет только уже загруженные шаблоны; `dict` собирает их контекст.
HTML-экранирование сохраняется, пользовательский текст не становится HTML.
Неизвестный компонент в конфигурации блока или ошибка синтаксиса останавливают запуск.

Шаблоны темы — доверенный код владельца проекта, их нельзя загружать от
посетителей сайта. Секреты не следует помещать в JSON темы: эти данные
предназначены для отображения.
