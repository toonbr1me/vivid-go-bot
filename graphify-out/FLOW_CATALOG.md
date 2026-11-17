# Полный каталог пользовательского flow

Узел содержит исходный файл и строку. Направления и условия связей — в flow.json.

## Запуск и входящие события

| Узел | Поведение | Источник |
| --- | --- | --- |
| `flow_boot` · Запуск единого процесса | Бот, HTTP и фоновые задачи работают под общим контекстом и errgroup. | [cmd/vivid/main.go](../cmd/vivid/main.go#L41) |
| `flow_schema` · Миграции и стартовый каталог | База открывается и схема применяется до чтения app_settings; каталог заполняется при отсутствии данных. | [internal/storage/postgres.go](../internal/storage/postgres.go#L26) |
| `flow_config` · BOT_STYLE и настройки | BOT_STYLE=legacy по умолчанию; STYLE запасное имя; html выбирает cards. BOT_CLEAN_CHAT=true по умолчанию. | [internal/config/config.go](../internal/config/config.go#L317) |
| `flow_polling` · Telegram long polling | Временная ошибка Telegram вызывает переподключение; HTTP и задания продолжают работать. | [internal/bot/bot.go](../internal/bot/bot.go#L112) |
| `flow_register` · Регистрация команд и событий | Команды, callback_query, widget_query, текст, вложения и события платежей. | [internal/bot/bot.go](../internal/bot/bot.go#L214) |
| `flow_start` · /start и deep links | ref_ фиксирует приглашение; link_ привязывает веб-аккаунт; затем основной экран. | [internal/bot/messages.go](../internal/bot/messages.go#L30) |
| `flow_callback` · Серверный маршрутизатор callback | Общий обработчик бизнес-действий для native-кнопок и проверенных HTML-действий. | [internal/bot/callbacks.go](../internal/bot/callbacks.go#L19) |
| `flow_text` · Входящий текст и состояние ввода | promo, ticket, ticket_reply, poll_question, poll_options, broadcast; неизвестный текст показывает подсказку и кнопки меню/поддержки. | [internal/bot/messages.go](../internal/bot/messages.go#L243) |
| `flow_input` · Временный диалог ввода: 20 минут | inputState хранится в памяти, не переживает перезапуск; это отдельно от сохранённой карточки и истории. | [internal/bot/bot.go](../internal/bot/bot.go#L37) |
| `flow_cancel` · /cancel и выход из раздела | Сбрасывает режим ввода; showScreen также очищает inputState. | [internal/bot/messages.go](../internal/bot/messages.go#L231) |
| `flow_command_start` · /start | Зарегистрированная команда → handleStart | [internal/bot/bot.go](../internal/bot/bot.go#L215) |
| `flow_command_menu` · /menu | Зарегистрированная команда → handleMenuCommand | [internal/bot/bot.go](../internal/bot/bot.go#L216) |
| `flow_command_buy` · /buy | Зарегистрированная команда → handleBuyCommand | [internal/bot/bot.go](../internal/bot/bot.go#L217) |
| `flow_command_trial` · /trial | Зарегистрированная команда → handleTrialCommand | [internal/bot/bot.go](../internal/bot/bot.go#L218) |
| `flow_command_subscription` · /subscription | Зарегистрированная команда → handleSubscriptionCommand | [internal/bot/bot.go](../internal/bot/bot.go#L219) |
| `flow_command_status` · /status | Зарегистрированная команда → handleStatusCommand | [internal/bot/bot.go](../internal/bot/bot.go#L220) |
| `flow_command_view` · /view | Зарегистрированная команда → handleViewCommand | [internal/bot/bot.go](../internal/bot/bot.go#L221) |
| `flow_command_devices` · /devices | Зарегистрированная команда → handleDevicesCommand | [internal/bot/bot.go](../internal/bot/bot.go#L222) |
| `flow_command_diagnostics` · /diagnostics | Зарегистрированная команда → handleDiagnosticsCommand | [internal/bot/bot.go](../internal/bot/bot.go#L223) |
| `flow_command_promo` · /promo | Зарегистрированная команда → handlePromoCommand | [internal/bot/bot.go](../internal/bot/bot.go#L224) |
| `flow_command_referral` · /referral | Зарегистрированная команда → handleReferralCommand | [internal/bot/bot.go](../internal/bot/bot.go#L225) |
| `flow_command_support` · /support | Зарегистрированная команда → handleSupportCommand | [internal/bot/bot.go](../internal/bot/bot.go#L226) |
| `flow_command_info` · /info | Зарегистрированная команда → handleInfoCommand | [internal/bot/bot.go](../internal/bot/bot.go#L227) |
| `flow_command_weblogin` · /weblogin | Зарегистрированная команда → handleWebLoginCommand | [internal/bot/bot.go](../internal/bot/bot.go#L228) |
| `flow_command_help` · /help | Зарегистрированная команда → handleHelpCommand | [internal/bot/bot.go](../internal/bot/bot.go#L229) |
| `flow_command_paysupport` · /paysupport | Зарегистрированная команда → handlePaySupportCommand | [internal/bot/bot.go](../internal/bot/bot.go#L230) |
| `flow_command_cancel` · /cancel | Зарегистрированная команда → handleCancelCommand | [internal/bot/bot.go](../internal/bot/bot.go#L231) |
| `flow_command_admin` · /admin | Зарегистрированная команда → handleAdmin | [internal/bot/bot.go](../internal/bot/bot.go#L233) |
| `flow_command_grant` · /grant | Зарегистрированная команда → handleGrant | [internal/bot/bot.go](../internal/bot/bot.go#L234) |
| `flow_command_tickets` · /tickets | Зарегистрированная команда → handleTickets | [internal/bot/bot.go](../internal/bot/bot.go#L235) |
| `flow_command_reply` · /reply | Зарегистрированная команда → handleReply | [internal/bot/bot.go](../internal/bot/bot.go#L236) |
| `flow_command_close` · /close | Зарегистрированная команда → handleCloseTicketCommand | [internal/bot/bot.go](../internal/bot/bot.go#L237) |
| `flow_command_stats` · /stats | Зарегистрированная команда → handleStats | [internal/bot/bot.go](../internal/bot/bot.go#L238) |
| `flow_command_polls` · /polls | Зарегистрированная команда → handlePollsCommand | [internal/bot/bot.go](../internal/bot/bot.go#L239) |
| `flow_command_announce` · /announce | Зарегистрированная команда → handleAnnounce | [internal/bot/bot.go](../internal/bot/bot.go#L240) |
| `flow_command_partners` · /partners | Зарегистрированная команда → handlePartnersCommand | [internal/bot/bot.go](../internal/bot/bot.go#L241) |

## Legacy: доставка и восстановление

| Узел | Поведение | Источник |
| --- | --- | --- |
| `flow_legacy` · BOT_STYLE=legacy → classic | Основной режим; не требует Widget API. Legacy здесь — режим текущего Go-бота, а не анализ Python-репозитория. | [internal/bot/views.go](../internal/bot/views.go#L15) |
| `flow_show` · Выбор рендерера showScreen | Выбирает специализированный rich, компактный native или обычный экран; HTML использует отдельную ветку. | [internal/bot/richscreens.go](../internal/bot/richscreens.go#L87) |
| `flow_richscreen` · Специализированный rich-экран | main, subscription, profile, history, settings, buy, devices; остальные поддержанные страницы через cardState → rich. | [internal/bot/rich_standard.go](../internal/bot/rich_standard.go#L15) |
| `flow_richsend` · sendRichMessage / editRichMessage | Кнопки встраиваются в документ только если выразимы все; иначе сохраняется inline-клавиатура. | [internal/bot/bot.go](../internal/bot/bot.go#L405) |
| `flow_richfallback` · Rich API недоступен | Отказ метода выключает rich transport; отдельный отказ документа допускает обычную доставку. | [internal/botui/legacy/transport/rich.go](../internal/botui/legacy/transport/rich.go#L77) |
| `flow_compact` · Компактный native-экран | main, subscription, devices, connect, diagnostics имеют специальное компактное отображение. | [internal/bot/card_screens.go](../internal/bot/card_screens.go#L212) |
| `flow_nativehtml` · Telegram HTML и inline-кнопки | SendMessage или EditMessageText. Это Telegram parse_mode HTML, не интерактивный HTML-виджет. | [internal/bot/bot.go](../internal/bot/bot.go#L327) |
| `flow_plain` · Обычный текст при ошибке разметки | Только ошибки markup вызывают plain fallback; сетевой отказ не превращается в новую отправку. | [internal/bot/bot.go](../internal/bot/bot.go#L314) |
| `flow_remember` · Сначала сохранить ID экрана | Старый экран удаляется только после успешной записи нового ID в БД. | [internal/bot/clean_chat.go](../internal/bot/clean_chat.go#L86) |
| `flow_clean` · Удаление обработанной команды | Только личный чат, BOT_CLEAN_CHAT и успешная доставка; ошибки сохраняют исходную команду. | [internal/bot/clean_chat.go](../internal/bot/clean_chat.go#L35) |
| `flow_markdown` · Markdown поддержки → rich / HTML chunks | Длинный Markdown делится по блокам при fallback; в cards преобразуется в textCard. | [internal/bot/bot.go](../internal/bot/bot.go#L450) |
| `flow_retire` · Убрать старый интерфейс после смены режима | После успешной команды очищает старый widget/native ID и прежний селектор. | [internal/bot/clean_chat.go](../internal/bot/clean_chat.go#L51) |

## HTML: карточка и проверка действий

| Узел | Поведение | Источник |
| --- | --- | --- |
| `flow_html` · BOT_STYLE=html → cards | Экспериментальный интерактивный вид; настройка владельца выше персонального bot_views. | [internal/bot/views.go](../internal/bot/views.go#L15) |
| `flow_interfaces` · Специализированные HTML-экраны | main/subscription, devices, connect, profile, buy, support, more, diagnostics. | [internal/bot/card_screens.go](../internal/bot/card_screens.go#L14) |
| `flow_textcard` · Остальные ответы → textCard | HTML/Markdown текста уплощается и разбивается; сохраняются callback, URL и CopyText действия. | [internal/bot/cards.go](../internal/bot/cards.go#L164) |
| `flow_prepare` · prepareCard и границы страницы | Проверяет страницы, компактность и индексы; данные текущей страницы ≤1024 байт. | [internal/bot/cards.go](../internal/bot/cards.go#L87) |
| `flow_appearance` · Шаблон, тема и SHA256 документа | Файл темы + BOT_UI_THEME в БД + BOT_HTML_THEME + проектный шаблон/CSS; хеш включает документ. | [internal/bot/card_appearance.go](../internal/bot/card_appearance.go#L52) |
| `flow_widgetlimits` · HTML ≤32 KiB / zlib ≤4 KiB | Слишком большой документ отклоняется сервером до отправки. | [internal/bot/card_appearance.go](../internal/bot/card_appearance.go#L25) |
| `flow_deliverui` · deliverUI: создать или изменить | Сохраняется полный cardState с серверными Actions; клиент видит только текущую страницу. | [internal/bot/cards.go](../internal/bot/cards.go#L241) |
| `flow_editwidget` · Изменить приватный chatData | Тот же ID, версия и appearance: обновление без новой карточки; ошибка edit возвращается. | [internal/bot/cards.go](../internal/bot/cards.go#L278) |
| `flow_newwidget` · SendWidget при новом документе | Команда при clean chat или новый appearance создаёт документ; старый удаляется после сохранения нового. | [internal/bot/cards.go](../internal/bot/cards.go#L285) |
| `flow_widgetsave` · Сохранить cardState и message ID | Состояние, действия и ревизия доступны после перезапуска. | [internal/storage/bot_cards.go](../internal/storage/bot_cards.go#L19) |
| `flow_widgetcompat` · INPUT_MEDIA_WIDGET_NOT_ALLOWED | Если есть совместимая сохранённая карточка, обновить её данные и сохранить прежний appearance; иначе ошибка. | [internal/bot/cards.go](../internal/bot/cards.go#L292) |
| `flow_widgeterror` · HTML не доставлен: сохранить выбранный режим | showScreen сообщает о сбое без автоматического переключения пользователя на legacy. | [internal/bot/richscreens.go](../internal/bot/richscreens.go#L100) |
| `flow_widgetclient` · createVividWidget и chatDataChanged | Клиент рисует серверное состояние; ожидание, ошибки, timeout и обновление ревизии. | [internal/bot/widgets/interface.html](../internal/bot/widgets/interface.html#L12) |
| `flow_widgetquery` · WidgetQuery: ui:revision:kind:value | Виды действий a=action, p=page, n=navigation. | [internal/bot/cards.go](../internal/bot/cards.go#L318) |
| `flow_widgetguard` · Проверить владельца, ID, ревизию и видимость | Только data-query в личном чате; From.ID=Chat.ID; текущий message; режим cards; допустимый индекс и видимая кнопка. | [internal/bot/cards.go](../internal/bot/cards.go#L313) |
| `flow_widgetlock` · Сериализовать действие карточки | DB lock предотвращает одновременное применение несовместимых состояний. | [internal/storage/bot_cards.go](../internal/storage/bot_cards.go#L30) |
| `flow_widgetstale` · Устаревшее действие отклонить | При неверной ревизии или старом ID бизнес-действие не выполняется. | [internal/bot/cards.go](../internal/bot/cards.go#L339) |
| `flow_widgetpage` · Пагинация / нижние вкладки | Проверяется диапазон страницы; вкладки subscription, devices, support, more ограничены сервером. | [internal/bot/cards.go](../internal/bot/cards.go#L393) |
| `flow_widgetaction` · Подтверждённое действие → общий callback | card:* идёт в cardAction; остальные callbacks повторно используют handleCallback. | [internal/bot/cards.go](../internal/bot/cards.go#L390) |
| `flow_copy` · Получить ссылку отдельным сообщением | Из-за защищённого widget clipboard: отдельное native-сообщение; CopyText ≤256 UTF-16, иначе code. | [internal/bot/card_native.go](../internal/bot/card_native.go#L14) |

## Экраны и пользовательская навигация

| Узел | Поведение | Источник |
| --- | --- | --- |
| `flow_guidance` · Приоритет следующего действия клиента | Сначала paid без выдачи, затем active/recurring/trial/expiry, потом pending, expired, trial и покупка. Общая модель сейчас включена в mainScreen, subscriptionCard и веб-аккаунт; richMain её не вызывает. | [internal/domain/customer.go](../internal/domain/customer.go#L10) |
| `flow_guidepaid` · Приоритет 1: оплачено, но не выдано | Следующее действие order: проверить выдачу, а не повторно покупать. | [internal/domain/customer.go](../internal/domain/customer.go#L11) |
| `flow_guideactive` · Активная подписка: выбрать нужный следующий шаг | Приоритет: recurring → trial connect → окончание за 24 часа → сохранённый срок при недоступной панели → обычное подключение. | [internal/domain/customer.go](../internal/domain/customer.go#L15) |
| `flow_guidepending` · Неактивный доступ: живой счёт ожидает оплаты | Предлагает открыть существующий заказ, если CanPay. | [internal/domain/customer.go](../internal/domain/customer.go#L30) |
| `flow_guideexpiry` · Неактивный доступ: известный срок | Будущий срок при неактивном статусе → support; прошедший срок → buy. | [internal/domain/customer.go](../internal/domain/customer.go#L22) |
| `flow_guidetrial` · Новый клиент: доступен trial | Предлагает бесплатный trial своего канала; иначе обычный выбор покупки. | [internal/domain/customer.go](../internal/domain/customer.go#L39) |
| `flow_screen_diagnostics` · nav:diagnostics | Зарегистрированный раздел navigationScreen; конкретное представление выбирает showScreen. | [internal/bot/callbacks.go](../internal/bot/callbacks.go#L248) |
| `flow_screen_main` · nav:main | Зарегистрированный раздел navigationScreen; конкретное представление выбирает showScreen. | [internal/bot/callbacks.go](../internal/bot/callbacks.go#L254) |
| `flow_screen_subscription` · nav:subscription | Зарегистрированный раздел navigationScreen; конкретное представление выбирает showScreen. | [internal/bot/callbacks.go](../internal/bot/callbacks.go#L256) |
| `flow_screen_buy` · nav:buy | Зарегистрированный раздел navigationScreen; конкретное представление выбирает showScreen. | [internal/bot/callbacks.go](../internal/bot/callbacks.go#L258) |
| `flow_screen_trial` · nav:trial | Зарегистрированный раздел navigationScreen; конкретное представление выбирает showScreen. | [internal/bot/callbacks.go](../internal/bot/callbacks.go#L260) |
| `flow_screen_devices` · nav:devices | Зарегистрированный раздел navigationScreen; конкретное представление выбирает showScreen. | [internal/bot/callbacks.go](../internal/bot/callbacks.go#L55) |
| `flow_screen_profile` · nav:profile | Зарегистрированный раздел navigationScreen; конкретное представление выбирает showScreen. | [internal/bot/callbacks.go](../internal/bot/callbacks.go#L264) |
| `flow_screen_history` · nav:history | Зарегистрированный раздел navigationScreen; конкретное представление выбирает showScreen. | [internal/bot/callbacks.go](../internal/bot/callbacks.go#L266) |
| `flow_screen_settings` · nav:settings | Зарегистрированный раздел navigationScreen; конкретное представление выбирает showScreen. | [internal/bot/callbacks.go](../internal/bot/callbacks.go#L144) |
| `flow_screen_promo` · nav:promo | Зарегистрированный раздел navigationScreen; конкретное представление выбирает showScreen. | [internal/bot/callbacks.go](../internal/bot/callbacks.go#L147) |
| `flow_screen_referral` · nav:referral | Зарегистрированный раздел navigationScreen; конкретное представление выбирает showScreen. | [internal/bot/callbacks.go](../internal/bot/callbacks.go#L272) |
| `flow_screen_support` · nav:support | Зарегистрированный раздел navigationScreen; конкретное представление выбирает showScreen. | [internal/bot/callbacks.go](../internal/bot/callbacks.go#L174) |
| `flow_screen_info` · nav:info | Зарегистрированный раздел navigationScreen; конкретное представление выбирает showScreen. | [internal/bot/callbacks.go](../internal/bot/callbacks.go#L276) |
| `flow_screen_help` · nav:help | Зарегистрированный раздел navigationScreen; конкретное представление выбирает showScreen. | [internal/bot/callbacks.go](../internal/bot/callbacks.go#L278) |
| `flow_screen_more` · nav:more | Зарегистрированный раздел navigationScreen; конкретное представление выбирает showScreen. | [internal/bot/callbacks.go](../internal/bot/callbacks.go#L280) |
| `flow_screen_connect` · nav:connect | Зарегистрированный раздел navigationScreen; конкретное представление выбирает showScreen. | [internal/bot/callbacks.go](../internal/bot/callbacks.go#L282) |

## Покупка, триал и промокоды

| Узел | Поведение | Источник |
| --- | --- | --- |
| `flow_plans` · Выбор тарифа и QuotePlans | Активные тарифы, промокод, цена, отказы; HTML buyCard и richBuy используют общую службу цен. | [internal/bot/screens.go](../internal/bot/screens.go#L188) |
| `flow_plan` · Оформление выбранного тарифа | plan:* сбрасывает opt-in автопродления, проверяет личный чат и строит карточку или planScreen. | [internal/bot/checkout_cards.go](../internal/bot/checkout_cards.go#L100) |
| `flow_checkbox` · Серверный opt-in автопродления | recurring:toggle хранится в БД по tgID и planID; checkbox в браузере не меняет условия самостоятельно. | [internal/bot/callbacks.go](../internal/bot/callbacks.go#L98) |
| `flow_payoptions` · Выбор шлюза и способа | Реестр фильтрует доступность; при recurring только допустимые provider/method/period. | [internal/payments/service.go](../internal/payments/service.go#L107) |
| `flow_checkout` · Создать или переиспользовать счёт | Проверяет тариф, subject, промо, сумму и provider; повтор живого заказа переиспользует ссылку. | [internal/payments/service.go](../internal/payments/service.go#L162) |
| `flow_invoice` · Экран счёта и внешняя оплата | URL оплаты, срок и check:orderID. Открытие URL само не подтверждает оплату. | [internal/bot/payments.go](../internal/bot/payments.go#L114) |
| `flow_balance` · Оплатить с внутреннего баланса | Списание и paid-заказ атомарны; при сбое выдачи заказ остаётся для повтора. | [internal/payments/admin.go](../internal/payments/admin.go#L20) |
| `flow_free` · Бесплатная покупка по промокоду | Нулевой итог создаёт paid-заказ без внешнего шлюза; recurring запрещён. | [internal/payments/service.go](../internal/payments/service.go#L317) |
| `flow_trial` · Пробный период своего канала | Различает bot/web; Telegram нужен для bot; активный доступ и повторное использование запрещены. | [internal/payments/service.go](../internal/payments/service.go#L287) |
| `flow_trialreserve` · Сохранить trial и заказ до выдачи | Стабильный trial-kind-subject ID продолжает ту же выдачу после сбоя; миграция не выдаёт второй триал. | [internal/payments/provision.go](../internal/payments/provision.go#L21) |
| `flow_promo` · Ввести и проверить промокод | Подарочные дни активируются отдельно; скидка сохраняется как intent и возвращает к тарифу. | [internal/bot/messages.go](../internal/bot/messages.go#L280) |
| `flow_promorules` · Проверки промокода | Формат, активность, срок, глобальные и персональные лимиты, тариф, повторная скидка. | [internal/payments/service.go](../internal/payments/service.go#L392) |
| `flow_gift` · Подарочный промокод → оплаченный gift-заказ | Повтор незавершённой выдачи продолжает заказ; одноразовые активации и журнал защищают подарок. | [internal/payments/service.go](../internal/payments/service.go#L453) |

## Подтверждение оплаты и выдача

| Узел | Поведение | Источник |
| --- | --- | --- |
| `flow_verify` · Проверенный webhook провайдера | Сигнатура/авторизация и проверка провайдера; только затем ReceiveEvent. | [internal/web/webhooks.go](../internal/web/webhooks.go#L16) |
| `flow_stars` · Telegram Stars: pre-checkout и success | Отдельный Telegram вход подтверждения; проверка заказа и суммы перед применением события. | [internal/bot/payments.go](../internal/bot/payments.go#L288) |
| `flow_inbox` · Сначала сохранить событие в payment inbox | Дедупликация хешем без времени приёма; сохранённое событие подтверждается, сбой применения повторяется отдельно. | [internal/payments/inbox.go](../internal/payments/inbox.go#L13) |
| `flow_event` · Сопоставить событие и заказ | Проверяет provider, amount, currency; неизвестные заказы inbox ждут, а не теряются. | [internal/payments/service.go](../internal/payments/service.go#L540) |
| `flow_pending` · pending: ожидаем оплату | Истёкший TTL меняет представление; проверить уже совершённый платёж всё ещё возможно. | [internal/domain/order_progress.go](../internal/domain/order_progress.go#L27) |
| `flow_paid` · paid: оплата подтверждена | Повторно платить не требуется; доступ пока не гарантирован. | [internal/domain/order_progress.go](../internal/domain/order_progress.go#L21) |
| `flow_delayed` · paid: выдача задерживается | Ошибка fulfillment или ≥2 минут после оплаты; финансовый статус остаётся paid. | [internal/domain/order_progress.go](../internal/domain/order_progress.go#L23) |
| `flow_failed` · failed / cancelled / refunded | Разные состояния отображения; фактический HandleEvent failed и cancelled у pending сохраняет failed. | [internal/domain/order_progress.go](../internal/domain/order_progress.go#L32) |
| `flow_check` · Проверить статус принадлежащего заказа | Повторная проверка владельца; CheckOrder; при ошибке refresh читаются подтверждённые данные. | [internal/bot/payments.go](../internal/bot/payments.go#L127) |
| `flow_reconcile` · Сверить платёж через API провайдера | Обрабатывает ожидающие платежи; дополняет webhook и ручную проверку. | [internal/payments/reconcile.go](../internal/payments/reconcile.go#L90) |
| `flow_provision` · Выдать paid-заказ по типу | Подписка/подарок или устройства/продление слотов; не создаёт новый платёж. | [internal/payments/service.go](../internal/payments/service.go#L526) |
| `flow_subjectlock` · Блокировка subject и повторное чтение | Уже provisioned — без повторной выдачи; неоплаченный заказ не исполняется. | [internal/payments/provision.go](../internal/payments/provision.go#L198) |
| `flow_days` · Зафиксировать дни в журнале | subscription_days фиксирует применённые дни и бонусы один раз. | [internal/payments/provision.go](../internal/payments/provision.go#L356) |
| `flow_target` · Зарезервировать абсолютный срок | subscription_target учитывает оплаченный срок и резервы параллельных заказов. | [internal/payments/provision.go](../internal/payments/provision.go#L395) |
| `flow_upgrade` · Trial → paid: профиль и трафик | Оплаченный профиль заменяет ограничения триала; необратимый сброс трафика отдельно защищён claim. | [internal/payments/provision.go](../internal/payments/provision.go#L249) |
| `flow_panel` · Создать / обновить профиль Remnawave | Абсолютный срок делает повтор идемпотентным; стабильное имя помогает восстановить create после таймаута. | [internal/payments/provision.go](../internal/payments/provision.go#L90) |
| `flow_postgrant` · Слоты, активация промо, награды | Отдельные journal.Once для слотов, promo_activation, referral_credit, partner_credit. | [internal/payments/provision.go](../internal/payments/provision.go#L426) |
| `flow_ready` · provisioned: доступ готов | Только после шагов выдачи фиксируется финальный статус; UI разделяет доступ, устройства и баланс. | [internal/payments/provision.go](../internal/payments/provision.go#L302) |
| `flow_retry` · Сохранённая очередь повторной выдачи | Сбой панели сохраняет paid; ScheduleProvisionRetry; фон и админ продолжают тот же заказ. | [internal/payments/service.go](../internal/payments/service.go#L599) |

## Автопродление и отмена

| Узел | Поведение | Источник |
| --- | --- | --- |
| `flow_capability` · Разрешённые recurring-возможности | Opt-in merchant; YooKassa card; Platega sbp, срок ≤730 дней; Wata/Stars здесь не поддержаны. | [internal/payments/recurring.go](../internal/payments/recurring.go#L17) |
| `flow_consent` · Зафиксировать согласие и исходные условия | Создание checkout с recurring сохраняет соглашение, сумму и период; обычная покупка согласия не даёт. | [internal/payments/service.go](../internal/payments/service.go#L270) |
| `flow_activation` · Ожидать подтверждение сохранённого метода | Первую покупку завершает клиент; рекуррентный webhook связывает внешний ID и отдельный заказ списания. | [internal/payments/recurring.go](../internal/payments/recurring.go#L87) |
| `flow_schedule` · Проверить расписание соглашения | Только включённые; pending-order не дублируется; активированность, внешний ID и время проверяются. | [internal/payments/recurring.go](../internal/payments/recurring.go#L179) |
| `flow_terms` · Остановить при изменении условий | Снятый тариф, новая цена или срок требуют нового согласия; reason=terms. | [internal/payments/recurring.go](../internal/payments/recurring.go#L242) |
| `flow_debit` · Один заказ на период списания | Стабильный ID периода; уже созданный link не создаётся снова; после 23 часов неизвестного результата — ручная сверка. | [internal/payments/recurring.go](../internal/payments/recurring.go#L258) |
| `flow_plategadebit` · Списание по расписанию Platega | Верифицированное внешнее событие получает свой неизменяемый заказ по транзакции. | [internal/payments/recurring.go](../internal/payments/recurring.go#L87) |
| `flow_cancelrec` · Отмена через общий сервис | Для Platega сначала успешная внешняя отмена; затем StopRecurring(user). Ошибка не изображается успехом. | [internal/payments/recurring.go](../internal/payments/recurring.go#L43) |
| `flow_stopped` · История: user / declined / terms / review | Отключённое соглашение остаётся видимым; платёж, отправленный до отмены, может быть законно завершён после неё. | [internal/domain/recurring.go](../internal/domain/recurring.go#L34) |

## Подключение и диагностика

| Узел | Поведение | Источник |
| --- | --- | --- |
| `flow_access` · Access: панель и сохранённый срок | Панель — источник текущего статуса, URL, трафика и устройств; при сбое БД сохраняет последний оплаченный срок. | [internal/payments/access.go](../internal/payments/access.go#L19) |
| `flow_connect` · Хаб подключения | Требует active, found и URL; частичный сбой списка устройств не скрывает известную ссылку. | [internal/bot/connect_hub.go](../internal/bot/connect_hub.go#L10) |
| `flow_apps` · Выбор Happ / Incy / Clash Mi / FlClashX | При публичном HTTPS сайта кнопки ведут на мостик импорта; иначе получить ссылку вручную. | [internal/bot/connect_hub.go](../internal/bot/connect_hub.go#L29) |
| `flow_bridge` · HTTPS-мостик → deep link приложения | Сайт проверяет входные параметры и предлагает открыть установленный клиент. | [internal/web/app_import.go](../internal/web/app_import.go#L31) |
| `flow_setup` · Платформа → установка → импорт | card:setup/platform/clients/import; один сценарий для native и HTML через deliverJourney. | [internal/bot/card_journey.go](../internal/bot/card_journey.go#L178) |
| `flow_diagnostics` · Проверить подписку и места | Проверяет доступ, срок, URL и места; не проверяет интернет-трафик на устройстве пользователя. | [internal/bot/diagnostics.go](../internal/bot/diagnostics.go#L12) |
| `flow_firstconnect` · Первое подключение из панели | Подписанное user.first_connected обновляет воронку; само нажатие кнопки подключения этим не является. | [internal/web/panel_webhook.go](../internal/web/panel_webhook.go#L85) |

## Устройства и дополнительные места

| Узел | Поведение | Источник |
| --- | --- | --- |
| `flow_devices` · Список устройств: по 3 на страницу | Native и HTML используют общую модель устройства; состояние списка может быть недоступно отдельно от срока. | [internal/bot/card_screens.go](../internal/bot/card_screens.go#L68) |
| `flow_deviceconfirm` · Выбор → подтверждение отвязки | HTML использует серверный полный HWID; legacy dev:rm использует проверяемый хвост, неоднозначность отклоняется. | [internal/bot/card_screens.go](../internal/bot/card_screens.go#L122) |
| `flow_detach` · Удалить HWID из панели | Удаляется запись устройства; подписка и профиль приложения остаются; при подключении устройство может вернуться. | [internal/payments/access.go](../internal/payments/access.go#L102) |
| `flow_slots` · Дополнительные места и продление | Пакеты и действующие grant slots; dev:buy/pay/renew вызывают общий checkout устройств. | [internal/bot/card_screens.go](../internal/bot/card_screens.go#L115) |
| `flow_devicequote` · Цена устройств до окончания подписки | Необходим действующий срок; цена и доступные пакеты рассчитываются службой. | [internal/payments/device.go](../internal/payments/device.go#L47) |
| `flow_devicecheckout` · Заказ устройств / продление grant | Проверки владения и пакета; оплаченный заказ исполняется отдельной веткой Provision. | [internal/payments/device.go](../internal/payments/device.go#L168) |
| `flow_devicelimit` · Синхронизация лимита панели | Учитывает базовый лимит и срок grant; периодическая задача пересчитывает истёкшие слоты. | [internal/payments/device.go](../internal/payments/device.go#L478) |

## Профиль, рефералы и авторизация

| Узел | Поведение | Источник |
| --- | --- | --- |
| `flow_profile` · Профиль и баланс | Баланс, состояние подписки, история и автопродление; rich/native/HTML представляют одни данные. | [internal/bot/account_cards.go](../internal/bot/account_cards.go#L9) |
| `flow_history` · История заказов | В боте последние 10; каждый заказ имеет проверку, поддержку и подтверждённый Progress. | [internal/bot/rich_standard.go](../internal/bot/rich_standard.go#L85) |
| `flow_referral` · Приглашение и режим награды | ref_ deep link; режим days/balance/both проверяется сервером; начисления при выдаче защищены журналом. | [internal/bot/screens.go](../internal/bot/screens.go#L568) |
| `flow_settings` · Настройки уведомлений | Expiry и при включённых заданиях activity; состояние хранится в БД. | [internal/bot/callbacks.go](../internal/bot/callbacks.go#L428) |
| `flow_weblogin` · /weblogin → одноразовый код | Код для авторизации сайта; не раскрывает токен бота. | [internal/bot/messages.go](../internal/bot/messages.go#L58) |
| `flow_google` · Google OAuth → subject | State и PKCE; аккаунт связан с subject и серверной сессией. | [internal/web/auth.go](../internal/web/auth.go#L263) |
| `flow_codeauth` · Код → серверная сессия | Одноразовый код обменивается на session cookie; csrf и rate limit. | [internal/web/auth.go](../internal/web/auth.go#L128) |
| `flow_session` · Cookie HttpOnly / Lax / Secure | В БД хранится хеш session token; Secure зависит от HTTPS. | [internal/web/auth.go](../internal/web/auth.go#L62) |
| `flow_link` · link_ код: объединить Telegram и web | Общие баланс, trials, referrals и заказы; конфликтующие panel profiles не сливаются молча. | [internal/bot/messages.go](../internal/bot/messages.go#L87) |

## Поддержка и история переписки

| Узел | Поведение | Источник |
| --- | --- | --- |
| `flow_support` · Помощь: диагностика / инструкция / обращение | Общий вход с заказов, настройки подключения или меню; staff дополнительно видит очередь. | [internal/bot/card_screens.go](../internal/bot/card_screens.go#L29) |
| `flow_newticket` · Создать обращение с контекстом | Новый диалог, проверенный order ID, необязательные приложение/OS; inputTicket ожидает текст/файл. | [internal/bot/callbacks.go](../internal/bot/callbacks.go#L174) |
| `flow_messagepersist` · Записать сообщение и вложение | Явно выбранный ticket не заменяется другим; закрытый не открывается устаревшим ответом. | [internal/bot/messages.go](../internal/bot/messages.go#L359) |
| `flow_attachment` · Фото / документ и подпись | Исходное Telegram-сообщение удаляется только после сохранения; вложение остаётся в истории. | [internal/bot/support_lifecycle.go](../internal/bot/support_lifecycle.go#L91) |
| `flow_cleaninput` · Убрать сохранённое входящее сообщение | Только clean chat и личный чат; вызывается после успешной записи, а не до неё. | [internal/bot/clean_chat.go](../internal/bot/clean_chat.go#L25) |
| `flow_ticket` · История обращения | Legacy — разворачиваемая цитата и части при превышении лимита; HTML ticketCard — страницы сообщений. | [internal/bot/support_lifecycle.go](../internal/bot/support_lifecycle.go#L23) |
| `flow_reply` · Ответить и продолжать диалог | inputTicket для владельца или inputTicketReply для сотрудника; несколько сообщений до выхода. | [internal/bot/callbacks.go](../internal/bot/callbacks.go#L589) |
| `flow_ai` · LLM или передача сотруднику | Знания и факты сервиса; персональные/чувствительные запросы, человек и неподходящие темы передаются staff. | [internal/support/assistant.go](../internal/support/assistant.go#L125) |
| `flow_staffreply` · Ответ сотрудника и внутренние заметки | Ответ сохраняется в выбранный открытый ticket; внутренние заметки не видны клиенту. | [internal/bot/messages.go](../internal/bot/messages.go#L442) |
| `flow_unread` · Счётчик последнего ответа и прочтения | Чтение связано с конкретным last staff message; фоновая доставка не снимает непрочитанное. | [internal/storage/support_lifecycle.go](../internal/storage/support_lifecycle.go#L9) |
| `flow_resolve` · Пользователь: проблема решена | Проверка владельца; закрытый ticket сохраняет историю и не открывается старой формой ответа. | [internal/bot/support_lifecycle.go](../internal/bot/support_lifecycle.go#L104) |
| `flow_webticket` · Веб-лента и авторизованные файлы | Владелец или staff; файлы скачиваются через сервер, без раскрытия bot token и file ID. | [internal/web/support.go](../internal/web/support.go#L134) |
| `flow_sse` · SSE / updates → видимая вкладка → read | Postgres уведомления будят ленту; JS дедуплицирует и отмечает просмотр в видимой вкладке. | [internal/web/live.go](../internal/web/live.go#L142) |

## Сотрудники и операции

| Узел | Поведение | Источник |
| --- | --- | --- |
| `flow_admin` · Пульт Telegram и веб-админка | Роли/permission из ADMIN_IDS и staff; чувствительные Telegram-действия только в личном чате. | [internal/bot/admin.go](../internal/bot/admin.go#L14) |
| `flow_attention` · Очередь проблем и повтор выдачи | AttentionOrders из всей очереди; список первых 6 в Telegram; retry только paid и orders.retry с аудитом. | [internal/bot/operations.go](../internal/bot/operations.go#L12) |
| `flow_compensate` · Компенсация: 3 дня на обращение | Открытый ticket, permission, один заказ на ticket, транзакция лимита/аудита/истории. | [internal/payments/compensation.go](../internal/payments/compensation.go#L11) |
| `flow_complimit` · Лимит компенсаций за 24 часа | Ограниченные сотрудники: дневной лимит; owner освобождён; при сбое панели лимит не освобождается. | [internal/storage/compensation.go](../internal/storage/compensation.go#L15) |
| `flow_broadcast` · Рассылка: аудитория → текст → предпросмотр | broadcast.send; frozen recipients; promo=КОД превращается в кнопку, повторно проверяется при применении. | [internal/bot/broadcasts.go](../internal/bot/broadcasts.go#L43) |
| `flow_broadcastconfirm` · Подтвердить черновик за 20 минут | Подтверждение один раз; отмена только draft; черновик и итоги сохранены. | [internal/bot/broadcasts.go](../internal/bot/broadcasts.go#L51) |
| `flow_broadcastdelivery` · Доставка: sent / failed / unknown / pending | Перед каждой доставкой проверить роль автора; 429 отложить; неоднозначный timeout/5xx не повторять автоматически. | [internal/bot/broadcasts.go](../internal/bot/broadcasts.go#L178) |
| `flow_polls` · Опросы: вопрос → варианты → ответы | Staff создаёт и рассылает; poll callbacks и результаты; отдельный сценарий от broadcast. | [internal/bot/polls.go](../internal/bot/polls.go#L92) |
| `flow_partners` · Партнёры, промокоды и приглашения | Управление партнёром staff; получатель приглашения входит в веб и привязывает аккаунт. | [internal/bot/partners.go](../internal/bot/partners.go#L117) |
| `flow_grant` · Ручная выдача дней | Авторизованная выдача через общую payments службу. | [internal/bot/admin.go](../internal/bot/admin.go#L171) |

## Веб-сайт и кабинет

| Узел | Поведение | Источник |
| --- | --- | --- |
| `flow_web` · HTTP-маршруты и middleware | Сайт — отдельное отображение, а не Telegram HTML-widget; auth, CSRF, rate limit и security headers. | [internal/web/web.go](../internal/web/web.go#L117) |
| `flow_landing` · Витрина → /buy | Тема из WEB_DIR или встроенные шаблоны; цены из службы, контент из theme.json. | [internal/web/pages.go](../internal/web/pages.go#L37) |
| `flow_webbuy` · GET/POST /buy и POST /trial | POST требует subject; consent берётся с формы; общие Checkout/баланс и GrantTrial. | [internal/web/pages.go](../internal/web/pages.go#L181) |
| `flow_payresult` · /pay/success и /pay/fail | Redirect — только вход на страницу; владелец заказа проверяется; показ подтверждённого Progress. | [internal/web/pages.go](../internal/web/pages.go#L292) |
| `flow_webaccount` · /account: доступ, заказы, промо, устройства | Общие payments Access/Guidance; POST проверки заказа, устройств, режима рефералов и отмены recurring. | [internal/web/account.go](../internal/web/account.go#L43) |
| `flow_websupport` · /support: гость или аккаунт | Гость оставляет контакт; authenticated пользователь создаёт диалог с проверенным контекстом. | [internal/web/support.go](../internal/web/support.go#L51) |
| `flow_webpartner` · /partner/claim → кабинет партнёра | Токен приглашения и аккаунт связывают партнёрство; награды идут на внутренний баланс. | [internal/web/partner.go](../internal/web/partner.go#L32) |
| `flow_webadmin` · /admin: роли и аудит | Очередь, сводка, promos, partners, tickets, audit; owner-операции отдельно от staff. | [internal/web/admin.go](../internal/web/admin.go#L104) |
| `flow_screenlab` · 42 демонстрационных сценария / screenlab | Демо-данные и точные рендереры; каталог не подтверждает production Widget API или реальные платежи. | [internal/bot/screen_catalog.go](../internal/bot/screen_catalog.go#L23) |
| `flow_route_0` · GET /{$} | HTTP маршрут → landing | [internal/web/web.go](../internal/web/web.go#L118) |
| `flow_route_1` · GET /buy | HTTP маршрут → buyPage | [internal/web/web.go](../internal/web/web.go#L119) |
| `flow_route_2` · POST /buy | HTTP маршрут → buySubmit; CSRF wrapper | [internal/web/web.go](../internal/web/web.go#L120) |
| `flow_route_3` · POST /trial | HTTP маршрут → trialSubmit; CSRF wrapper | [internal/web/web.go](../internal/web/web.go#L121) |
| `flow_route_4` · GET /pay/success | HTTP маршрут → paySuccess | [internal/web/web.go](../internal/web/web.go#L122) |
| `flow_route_5` · GET /pay/fail | HTTP маршрут → payFail | [internal/web/web.go](../internal/web/web.go#L123) |
| `flow_route_6` · GET /login | HTTP маршрут → loginPage | [internal/web/web.go](../internal/web/web.go#L125) |
| `flow_route_7` · POST /auth/code | HTTP маршрут → loginByCode; CSRF wrapper | [internal/web/web.go](../internal/web/web.go#L126) |
| `flow_route_8` · GET /auth/google | HTTP маршрут → googleLogin | [internal/web/web.go](../internal/web/web.go#L127) |
| `flow_route_9` · GET /auth/google/callback | HTTP маршрут → googleCallback | [internal/web/web.go](../internal/web/web.go#L128) |
| `flow_route_10` · POST /logout | HTTP маршрут → logout; CSRF wrapper | [internal/web/web.go](../internal/web/web.go#L129) |
| `flow_route_11` · GET /account | HTTP маршрут → accountPage | [internal/web/web.go](../internal/web/web.go#L131) |
| `flow_route_12` · POST /account/promo | HTTP маршрут → accountPromo; CSRF wrapper | [internal/web/web.go](../internal/web/web.go#L132) |
| `flow_route_13` · POST /account/referral-mode | HTTP маршрут → accountReferralMode; CSRF wrapper | [internal/web/web.go](../internal/web/web.go#L133) |
| `flow_route_14` · POST /account/orders/check | HTTP маршрут → accountCheckOrder; CSRF wrapper | [internal/web/web.go](../internal/web/web.go#L134) |
| `flow_route_15` · POST /account/recurring/cancel | HTTP маршрут → accountCancelRecurring; CSRF wrapper | [internal/web/web.go](../internal/web/web.go#L135) |
| `flow_route_16` · POST /account/devices/detach | HTTP маршрут → accountDetachDevice; CSRF wrapper | [internal/web/web.go](../internal/web/web.go#L136) |
| `flow_route_17` · POST /account/devices/buy | HTTP маршрут → accountBuyDevices; CSRF wrapper | [internal/web/web.go](../internal/web/web.go#L137) |
| `flow_route_18` · POST /account/devices/renew | HTTP маршрут → accountRenewDevices; CSRF wrapper | [internal/web/web.go](../internal/web/web.go#L138) |
| `flow_route_19` · GET /connect | HTTP маршрут → connectPage | [internal/web/web.go](../internal/web/web.go#L140) |
| `flow_route_20` · GET /support | HTTP маршрут → supportPage | [internal/web/web.go](../internal/web/web.go#L141) |
| `flow_route_21` · POST /support | HTTP маршрут → supportSubmit; CSRF wrapper | [internal/web/web.go](../internal/web/web.go#L142) |
| `flow_route_22` · GET /support/tickets/{id} | HTTP маршрут → ticketPage | [internal/web/web.go](../internal/web/web.go#L143) |
| `flow_route_23` · GET /support/tickets/{id}/updates | HTTP маршрут → ticketStream | [internal/web/web.go](../internal/web/web.go#L144) |
| `flow_route_24` · POST /support/tickets/{id} | HTTP маршрут → ticketReply; CSRF wrapper | [internal/web/web.go](../internal/web/web.go#L145) |
| `flow_route_25` · POST /support/tickets/{id}/resolve | HTTP маршрут → ticketResolve; CSRF wrapper | [internal/web/web.go](../internal/web/web.go#L146) |
| `flow_route_26` · POST /support/tickets/{id}/compensate | HTTP маршрут → ticketCompensate; CSRF wrapper | [internal/web/web.go](../internal/web/web.go#L147) |
| `flow_route_27` · POST /support/tickets/{id}/read | HTTP маршрут → ticketRead; CSRF wrapper | [internal/web/web.go](../internal/web/web.go#L148) |
| `flow_route_28` · GET /support/tickets/{id}/files/{message} | HTTP маршрут → ticketFile | [internal/web/web.go](../internal/web/web.go#L149) |
| `flow_route_29` · GET /terms | HTTP маршрут → termsPage | [internal/web/web.go](../internal/web/web.go#L151) |
| `flow_route_30` · GET /privacy | HTTP маршрут → privacyPage | [internal/web/web.go](../internal/web/web.go#L152) |
| `flow_route_31` · GET /happ | HTTP маршрут → happBridge | [internal/web/web.go](../internal/web/web.go#L153) |
| `flow_route_32` · GET /app-import/{app} | HTTP маршрут → appImport | [internal/web/web.go](../internal/web/web.go#L154) |
| `flow_route_33` · GET /static/bot-mascot/{name} | HTTP маршрут → botMascot | [internal/web/web.go](../internal/web/web.go#L155) |
| `flow_route_34` · GET /partner | HTTP маршрут → partnerCabinet | [internal/web/web.go](../internal/web/web.go#L156) |
| `flow_route_35` · GET /partner/claim | HTTP маршрут → partnerClaim | [internal/web/web.go](../internal/web/web.go#L157) |
| `flow_route_36` · POST /partner/claim | HTTP маршрут → partnerClaim; CSRF wrapper | [internal/web/web.go](../internal/web/web.go#L158) |
| `flow_route_37` · GET /admin | HTTP маршрут → adminHome | [internal/web/web.go](../internal/web/web.go#L161) |
| `flow_route_38` · GET /admin/promos | HTTP маршрут → adminPromosPage | [internal/web/web.go](../internal/web/web.go#L162) |
| `flow_route_39` · POST /admin/promos | HTTP маршрут → adminPromoCreate; CSRF wrapper | [internal/web/web.go](../internal/web/web.go#L163) |
| `flow_route_40` · POST /admin/promos/{code}/toggle | HTTP маршрут → adminPromoToggle; CSRF wrapper | [internal/web/web.go](../internal/web/web.go#L164) |
| `flow_route_41` · POST /admin/promos/{code}/partner | HTTP маршрут → adminPromoPartner; CSRF wrapper | [internal/web/web.go](../internal/web/web.go#L165) |
| `flow_route_42` · GET /admin/partners | HTTP маршрут → adminPartnersPage | [internal/web/web.go](../internal/web/web.go#L166) |
| `flow_route_43` · POST /admin/partners | HTTP маршрут → adminPartnerCreate; CSRF wrapper | [internal/web/web.go](../internal/web/web.go#L167) |
| `flow_route_44` · GET /admin/partners/{id} | HTTP маршрут → adminPartnerPage | [internal/web/web.go](../internal/web/web.go#L168) |
| `flow_route_45` · POST /admin/partners/{id} | HTTP маршрут → adminPartnerUpdate; CSRF wrapper | [internal/web/web.go](../internal/web/web.go#L169) |
| `flow_route_46` · POST /admin/partners/{id}/invite | HTTP маршрут → adminPartnerInvite; CSRF wrapper | [internal/web/web.go](../internal/web/web.go#L170) |
| `flow_route_47` · GET /admin/tickets | HTTP маршрут → adminTicketsPage | [internal/web/web.go](../internal/web/web.go#L171) |
| `flow_route_48` · GET /admin/audit | HTTP маршрут → adminAuditPage | [internal/web/web.go](../internal/web/web.go#L172) |
| `flow_route_49` · GET /admin/orders | HTTP маршрут → adminOrdersPage | [internal/web/web.go](../internal/web/web.go#L173) |
| `flow_route_50` · POST /admin/orders/{id}/retry | HTTP маршрут → adminOrderRetry; CSRF wrapper | [internal/web/web.go](../internal/web/web.go#L174) |
| `flow_route_51` · GET /admin/screens | HTTP маршрут → adminScreensPage | [internal/web/web.go](../internal/web/web.go#L175) |
| `flow_route_52` · GET /admin/appearance | HTTP маршрут → adminAppearancePage | [internal/web/web.go](../internal/web/web.go#L176) |
| `flow_route_53` · POST /admin/appearance | HTTP маршрут → adminAppearanceSave; CSRF wrapper | [internal/web/web.go](../internal/web/web.go#L177) |
| `flow_route_54` · GET /static/bot-widget-runtime.js | HTTP маршрут → widgetRuntime | [internal/web/web.go](../internal/web/web.go#L178) |
| `flow_route_55` · POST /webhooks/{provider} | HTTP маршрут → paymentWebhook | [internal/web/web.go](../internal/web/web.go#L185) |
| `flow_route_56` · POST /webhooks/remnawave | HTTP маршрут → panelWebhook | [internal/web/web.go](../internal/web/web.go#L186) |
| `flow_route_57` · / | HTTP маршрут → notFound | [internal/web/web.go](../internal/web/web.go#L192) |

## Фоновые задачи и уведомления

| Узел | Поведение | Источник |
| --- | --- | --- |
| `flow_jobs` · Фоновые задачи под общим контекстом | each every запускает задачу сразу, затем периодически; ошибка итерации не останавливает цикл. | [internal/jobs/jobs.go](../internal/jobs/jobs.go#L35) |
| `flow_paymentjob` · Каждую минуту: деньги и выдача | ReconcilePayments(25) → ProcessPaymentInbox(50) → ProvisionPending(25) → ProcessRecurring(25). | [internal/jobs/jobs.go](../internal/jobs/jobs.go#L53) |
| `flow_expiryjob` · 15 минут: напоминание о сроке | Окно последних 24 часов и настройки; recurring имеет отдельный текст без двойного призыва оплатить. | [internal/jobs/jobs.go](../internal/jobs/jobs.go#L137) |
| `flow_unpaidjob` · 30 минут: неоплаченный счёт | При включённом REMIND_UNPAID_INVOICE_HOURS и activity notification; claims хранятся в БД. Напоминание не подключившимся не зарегистрировано в jobs. | [internal/jobs/jobs.go](../internal/jobs/jobs.go#L197) |
| `flow_devicejob` · 10 минут: пересчитать лимиты устройств | Истечение пакетов и синхронизация панели. | [internal/jobs/jobs.go](../internal/jobs/jobs.go#L239) |
| `flow_aijob` · 20 секунд: помощник; час: idle | Handle и CloseIdle при включённой модели; idle передаётся сотрудникам. | [internal/jobs/jobs.go](../internal/jobs/jobs.go#L91) |
| `flow_paneljob` · 5 секунд: очередь уведомлений панели | Подпись HMAC, дедуп тела, окно 5 минут; неизвестным TG-профилям не отправлять; 429 отложить. | [internal/bot/panel_events.go](../internal/bot/panel_events.go#L11) |
| `flow_broadcastjob` · 2 секунды: очередь рассылки | До 20 получателей за итерацию; обработка ролей и неоднозначной доставки. | [internal/jobs/jobs.go](../internal/jobs/jobs.go#L38) |

## Сохранённое состояние и миграции

| Узел | Поведение | Источник |
| --- | --- | --- |
| `flow_subject` · Subject: единый владелец данных | Связывает Telegram, account, баланс, trials, referrals, orders; merged_into сохраняет историю объединения. | [internal/storage/subjects.go](../internal/storage/subjects.go#L37) |
| `flow_subjectmigration` · Перенос subject_id из Python-схемы | Идемпотентная миграция под блокировкой сохраняет прежние trial и баланс. | [internal/storage/subjects_migration.go](../internal/storage/subjects_migration.go#L52) |
| `flow_ordersdb` · orders и payment inbox | Финансовый статус, владелец, paid/provisioned timestamps, retry; не выводятся из интерфейса. | [internal/storage/orders.go](../internal/storage/orders.go#L61) |
| `flow_journal` · Журнал идемпотентной выдачи | Отдельные зафиксированные результаты шагов; внешнее изменение использует абсолютные параметры. | [internal/storage/journal.go](../internal/storage/journal.go#L71) |
| `flow_cardsdb` · bot_cards / bot_views / checkout preference | Переживает перезапуск в отличие от inputState в памяти; приватные действия не принимаются из браузера как истина. | [internal/storage/recurring.go](../internal/storage/recurring.go#L98) |
| `flow_supportdb` · support_tickets и support_messages | История, статус, контекст и вложения; separate read markers и internal notes. | [internal/storage/support.go](../internal/storage/support.go#L82) |
| `flow_operationsdb` · Рассылки, panel events, компенсации | Очереди, лимиты и результаты сохраняются в PostgreSQL, не в памяти воркера. | [internal/storage/migrations/0008_legacy_operations.sql](../internal/storage/migrations/0008_legacy_operations.sql#L1) |

## Найденные расхождения flow

| Узел | Поведение | Источник |
| --- | --- | --- |
| `flow_gapguidance` · Rich main не использует CustomerGuidance | Наблюдение: paid/order next-step добавляется в mainScreen и HTML subscriptionCard, но основной richMain не вызывает CustomerGuidance. Нужна проверка parity главного экрана. | [internal/bot/richscreens.go](../internal/bot/richscreens.go#L148) |
| `flow_gapreminder` · Настройка not connected без фонового задания | Поле читается и влияет на activity toggle; в RunWithAssistant нет corresponding reminder. Наблюдение статического анализа. | [internal/config/config.go](../internal/config/config.go#L430) |
| `flow_gaprecurringcopy` · Berloga обещает отсутствие автосписаний | Безусловный текст конфликтует с поддержкой recurring в buy/account при включении функции. | [web/themes/berloga/templates/landing.html](../web/themes/berloga/templates/landing.html#L6) |
| `flow_gaptickets` · Очередь admin tickets без перехода к ticket | Строка содержит текст темы и ID, но не ссылку на существующий staff ticket route. | [internal/web/templates/admin_tickets.html](../internal/web/templates/admin_tickets.html#L25) |
| `flow_gapfree` · Free flash обещает выданный доступ преждевременно | PurchaseWithBalance может вернуть paid при ошибке панели; finishCheckout всё равно пишет доступ выдан. Сам account Progress показывает подтверждённое состояние. | [internal/web/pages.go](../internal/web/pages.go#L244) |

## Развитие HTML: предложения

| Узел | Поведение | Источник |
| --- | --- | --- |
| `flow_parity` · Предложение: единая матрица состояний UI | INFERRED: сверять legacy rich, HTML и website по одинаковым данным и допустимым действиям; не считать наличие шаблона полной готовностью. | [internal/bot/card_screens.go](../internal/bot/card_screens.go#L14) |
| `flow_realwidget` · Предложение: production smoke HTML в Telegram | INFERRED: проверить разрешение SendWidget, отправку/обновление, copy, stale actions, тему и восстановление на реальном сервере. | [docs/bot-interface.md](../docs/bot-interface.md#L7) |
| `flow_copyreview` · Предложение: согласовать тексты и инструкции | INFERRED: убрать безусловное обещание отсутствия автосписаний при включённом recurring; унифицировать поддерживаемые клиенты. | [web/themes/berloga/templates/landing.html](../web/themes/berloga/templates/landing.html#L6) |
| `flow_inputresume` · Предложение: явно восстанавливать незавершённый ввод | INFERRED: решить, нужен ли DB draft для promo/support/polls/broadcast; сейчас inputState теряется при рестарте. | [internal/bot/bot.go](../internal/bot/bot.go#L57) |
