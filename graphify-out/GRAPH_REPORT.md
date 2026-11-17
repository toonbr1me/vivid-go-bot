# Vivid: карта кода и пользовательских сценариев

Срез рабочей копии на 2026-11-16, включая незакоммиченные изменения. См. FLOW_ANALYSIS.md для ручного анализа и flow.html для карты сценариев.

**Ограничения:** SQL AST недоступен (tree_sitter_sql не установлен); схема представлена семантически и через репозитории. PNG/WebP и лицензии шрифтов исключены из смысловой карты. AST вызовы не равны выполненным runtime-переходам. Связи INFERRED — выводы анализа; proposed — предложения. Токены сессии не измерены, нули в служебном отчёте не означают отсутствие затрат.

# Graph Report - vivid-go-bot  (2026-11-16)

## Corpus Check
- 323 files · ~180,575 words
- Verdict: corpus is large enough that graph structure adds value.
- Unclassified: 21 file(s) not represented in the graph (top: .css 8, .woff2 6, (none) 5)

## Summary
- 2837 nodes · 14162 edges · 83 communities (70 shown, 13 thin omitted)
- Extraction: 83% EXTRACTED · 17% INFERRED · 0% AMBIGUOUS · INFERRED: 2349 edges (avg confidence: 0.81)
- Token cost: 0 input · 0 output

## Community Hubs (Navigation)
- Код: internal/bot
- Код: internal/storage
- Код: internal/storage
- Код: internal/remnawave
- Фоновые задачи и уведомления
- Код: internal/payments
- Код: internal/payments
- Код: internal/mdhtml
- Код: internal/bot
- Код: internal/storage
- Веб-сайт и кабинет
- Код: internal/payments
- Запуск и входящие события
- Устройства и дополнительные места
- Код: internal/bot
- Код: internal/bot
- Сохранённое состояние и миграции
- Подтверждение оплаты и выдача
- Код: internal/bot
- HTML: карточка и проверка действий
- Подключение и диагностика
- Код: internal/web
- Развитие HTML: предложения
- Веб-сайт и кабинет
- Код: .github/workflows
- Подтверждение оплаты и выдача
- Код: internal/bot
- Код: deploy/development
- Покупка, триал и промокоды
- Поддержка и история переписки
- Сохранённое состояние и миграции
- Код: internal/web
- Код: docker-compose.yml
- Код: internal/bot/texts
- Код: internal/web
- Код: internal/web
- Сотрудники и операции
- Код: internal/payments
- Код: internal/web
- Код: internal/web/static
- Код: internal/config
- HTML: карточка и проверка действий
- Код: internal/web
- Код: internal/bot/texts
- Код: internal/support
- Код: internal/appearance
- Код: internal/botui/legacy/transport
- Код: internal/domain
- Поддержка и история переписки
- Сохранённое состояние и миграции
- HTML: карточка и проверка действий
- Сотрудники и операции
- Код: internal/bot
- Код: internal/payments
- Код: internal/web
- Код: internal/storage
- Код: sqlc.yaml
- Код: internal/bot
- Код: internal/config
- Код: internal/bot
- Код: internal/bot
- Legacy: доставка и восстановление
- Код: internal/support
- Код: internal/web
- Код: internal/botui/legacy
- Код: internal/web/static
- Устройства и дополнительные места
- Код: internal/domain
- Код: internal/llm
- Код: internal/web
- Код: internal/web
- Код: internal/web/static
- Код: internal/web
- Код: web/themes/berloga/static/berloga
- Код: go.mod

## God Nodes (most connected - your core abstractions)
1. `Bot` - 227 edges
2. `Server` - 110 edges
3. `Open()` - 108 edges
4. `button()` - 98 edges
5. `keyboard()` - 96 edges
6. `Money` - 91 edges
7. `HTTP-маршруты и middleware` - 89 edges
8. `Subject` - 60 edges
9. `Order` - 59 edges
10. `FromRubles()` - 57 edges

## Surprising Connections (you probably didn't know these)
- `Реферальные дни или баланс` --documents_or_renders--> `Приглашение и режим награды`  [INFERRED]
  README.md → internal/bot/screens.go
- `Legacy-история в expandable-цитате` --documents_or_renders--> `BOT_STYLE=legacy → classic`  [INFERRED]
  docs/customer-flow.md → internal/bot/views.go
- `Переход trial → paid` --documents_or_renders--> `Пробный период своего канала`  [INFERRED]
  docs/customer-logic-fixes.md → internal/payments/service.go
- `Остановка вне окна идемпотентности` --documents_or_renders--> `Выдать paid-заказ по типу`  [INFERRED]
  docs/recurring-payments.md → internal/payments/service.go
- `Аренда доставки уведомления` --documents_or_renders--> `Фоновые задачи под общим контекстом`  [INFERRED]
  docs/reliability.md → internal/jobs/jobs.go

## Import Cycles
- 2-file cycle: `internal/storage/customer_lifecycle_test.go -> internal/testdb/testdb.go -> internal/storage/customer_lifecycle_test.go`
- 2-file cycle: `internal/storage/accounts_test.go -> internal/storage/legacy_operations_test.go -> internal/storage/accounts_test.go`
- 2-file cycle: `internal/storage/customer_regression_test.go -> internal/storage/legacy_operations_test.go -> internal/storage/customer_regression_test.go`
- 2-file cycle: `internal/storage/legacy_operations_test.go -> internal/storage/recurring_safety_test.go -> internal/storage/legacy_operations_test.go`
- 3-file cycle: `internal/storage/accounts_test.go -> internal/storage/customer_lifecycle_test.go -> internal/testdb/testdb.go -> internal/storage/accounts_test.go`
- 3-file cycle: `internal/storage/customer_lifecycle_test.go -> internal/testdb/testdb.go -> internal/storage/customer_regression_test.go -> internal/storage/customer_lifecycle_test.go`
- 3-file cycle: `internal/storage/customer_lifecycle_test.go -> internal/testdb/testdb.go -> internal/storage/legacy_operations_test.go -> internal/storage/customer_lifecycle_test.go`
- 3-file cycle: `internal/storage/customer_lifecycle_test.go -> internal/testdb/testdb.go -> internal/storage/recurring_safety_test.go -> internal/storage/customer_lifecycle_test.go`
- 3-file cycle: `internal/storage/customer_lifecycle_test.go -> internal/testdb/testdb.go -> internal/storage/storage_test.go -> internal/storage/customer_lifecycle_test.go`
- 3-file cycle: `internal/storage/accounts_test.go -> internal/storage/customer_regression_test.go -> internal/storage/legacy_operations_test.go -> internal/storage/accounts_test.go`
- 3-file cycle: `internal/storage/accounts_test.go -> internal/storage/recurring_safety_test.go -> internal/storage/legacy_operations_test.go -> internal/storage/accounts_test.go`
- 3-file cycle: `internal/storage/accounts_test.go -> internal/storage/storage_test.go -> internal/storage/legacy_operations_test.go -> internal/storage/accounts_test.go`
- 3-file cycle: `internal/storage/accounts_test.go -> internal/testdb/testdb.go -> internal/storage/legacy_operations_test.go -> internal/storage/accounts_test.go`
- 3-file cycle: `internal/storage/accounts_test.go -> internal/storage/legacy_operations_test.go -> internal/storage/customer_regression_test.go -> internal/storage/accounts_test.go`
- 3-file cycle: `internal/storage/customer_regression_test.go -> internal/storage/recurring_safety_test.go -> internal/storage/legacy_operations_test.go -> internal/storage/customer_regression_test.go`
- 3-file cycle: `internal/storage/customer_regression_test.go -> internal/storage/storage_test.go -> internal/storage/legacy_operations_test.go -> internal/storage/customer_regression_test.go`
- 3-file cycle: `internal/storage/customer_regression_test.go -> internal/testdb/testdb.go -> internal/storage/legacy_operations_test.go -> internal/storage/customer_regression_test.go`
- 3-file cycle: `internal/storage/accounts_test.go -> internal/storage/legacy_operations_test.go -> internal/storage/recurring_safety_test.go -> internal/storage/accounts_test.go`
- 3-file cycle: `internal/storage/customer_regression_test.go -> internal/storage/legacy_operations_test.go -> internal/storage/recurring_safety_test.go -> internal/storage/customer_regression_test.go`
- 3-file cycle: `internal/storage/legacy_operations_test.go -> internal/storage/recurring_safety_test.go -> internal/storage/storage_test.go -> internal/storage/legacy_operations_test.go`

## Communities (83 total, 13 thin omitted)

### Community 0 - "Код: internal/bot"
Cohesion: 0.17
Nodes (12): deliveryMark, deliveryMarkKey, incomingMessageKey, keepMessageKey, newCardKey, promoPlanKey, ticketRefKey, subjectBackfill (+4 more)

### Community 1 - "Код: internal/storage"
Cohesion: 0.02
Nodes (18): operationToken(), validAudience(), PanelEvent, nullString(), randomHex(), NotificationSettings, quoteIdent(), resetSchema() (+10 more)

### Community 2 - "Код: internal/storage"
Cohesion: 0.03
Nodes (108): TestConfiguredStyleOverridesPreviousUserPreference(), grouped(), TestBytesText(), TestDaysLeft(), TestDaysWord(), TestDevicesWord(), TestEscapeHTML(), TestExpiryBar() (+100 more)

### Community 3 - "Код: internal/remnawave"
Cohesion: 0.04
Nodes (33): journeyPanel, lifecyclePanel, unavailableDeviceListPanel, widgetSnapshotPanel, panelDeviceLimit(), Panel, decodeUser(), decodeUsers() (+25 more)

### Community 4 - "Фоновые задачи и уведомления"
Cohesion: 0.04
Nodes (61): main(), serveLab(), ignoreCancel(), main(), newLogger(), resolveBotUsername(), run(), seedPacks() (+53 more)

### Community 5 - "Код: internal/payments"
Cohesion: 0.06
Nodes (27): decodeJSON(), errorText(), headerValue(), jsonString(), majorString(), moneyFromMajor(), parseISOTime(), parseMajorCents() (+19 more)

### Community 6 - "Код: internal/payments"
Cohesion: 0.04
Nodes (73): PromoOutcome, TestPriceText(), TestPriceTextKeepsAmountTogether(), ApplyDiscount(), FromRubles(), QuotePrice(), TestApplyDiscount(), TestMoneyString() (+65 more)

### Community 7 - "Код: internal/mdhtml"
Cohesion: 0.07
Nodes (61): cardButton, cardPage, cardStat, bytePrefix(), blockEnd(), Chunks(), codeBlock(), escape() (+53 more)

### Community 8 - "Код: internal/bot"
Cohesion: 0.09
Nodes (21): deliveryResult(), nativeCardScreen(), TestUniversalScreenUsesRichDelivery(), TestRichCardDocumentEscapesContentAndKeepsDevicePaging(), shortID(), parsePollOptions(), quietRichKeyboard(), richCardDocument() (+13 more)

### Community 9 - "Код: internal/storage"
Cohesion: 0.04
Nodes (34): parseMoney(), DeviceQuote, DeviceSlotView, Money, TestMoneyRubles(), PartnerReward, PartnerSummary, DefaultDevicePacks() (+26 more)

### Community 10 - "Веб-сайт и кабинет"
Cohesion: 0.05
Nodes (68): Предложение: согласовать тексты и инструкции, Berloga обещает отсутствие автосписаний, Очередь admin tickets без перехода к ticket, Google OAuth → subject, Витрина → /buy, Создать обращение с контекстом, GET /{$}, GET /buy (+60 more)

### Community 11 - "Код: internal/payments"
Cohesion: 0.08
Nodes (22): Checkout, ProvisionResult, Subject, Catalogue(), Plan, SortedPlans(), TestSortedPlans(), TestSortedPlansBreaksTieByID() (+14 more)

### Community 12 - "Запуск и входящие события"
Cohesion: 0.04
Nodes (64): Access: панель и сохранённый срок, Серверный маршрутизатор callback, /cancel и выход из раздела, Отмена через общий сервис, Проверить статус принадлежащего заказа, Код → серверная сессия, /admin, /buy (+56 more)

### Community 13 - "Устройства и дополнительные места"
Cohesion: 0.06
Nodes (59): Удалить HWID из панели, Выбор → подтверждение отвязки, Список устройств: по 3 на страницу, Помощь: диагностика / инструкция / обращение, Доступ активен, Оплаченный срок требует проверки, Персональная ссылка подписки, Нет активной подписки (+51 more)

### Community 14 - "Код: internal/bot"
Cohesion: 0.13
Nodes (4): Bot, deepLinkArg(), queryEscape(), ParseStarsPayload()

### Community 15 - "Код: internal/bot"
Cohesion: 0.11
Nodes (8): disabledPreview(), ignoreNotModified(), isMarkupError(), broadcastStatus(), markDelivered(), forceNewCard(), checkoutError(), deviceError()

### Community 16 - "Сохранённое состояние и миграции"
Cohesion: 0.06
Nodes (20): Subject: единый владелец данных, Перенос subject_id из Python-схемы, reservedExpiry(), nullStringPtr(), createOrder(), partnerRewardCount(), recordPromoActivation(), usePromo() (+12 more)

### Community 17 - "Подтверждение оплаты и выдача"
Cohesion: 0.05
Nodes (21): inputState, AccessDevice, OrderProgress, PartnerInviteCode, Зафиксировать дни в журнале, Журнал идемпотентной выдачи, Создать / обновить профиль Remnawave, Зарезервировать абсолютный срок (+13 more)

### Community 18 - "Код: internal/bot"
Cohesion: 0.11
Nodes (24): currencySymbol(), dateText(), dateTimeText(), daysLeft(), daysWord(), devicesWord(), escapeHTML(), expiryBar() (+16 more)

### Community 19 - "HTML: карточка и проверка действий"
Cohesion: 0.05
Nodes (46): InvoiceLinker, prepareCard и границы страницы, Остальные ответы → textCard, New(), TestCheckoutToggleFiltersUnsupportedMethods(), TestClassicConnectionJourneyKeepsNativeTransport(), TestCopyHandoffKeepsTheActiveCardAndUsesUnprotectedMessage(), TestOldWidgetCodeIsReplacedOnce() (+38 more)

### Community 20 - "Подключение и диагностика"
Cohesion: 0.06
Nodes (44): Выбор Happ / Incy / Clash Mi / FlClashX, HTTPS-мостик → deep link приложения, Хаб подключения, Слоты, активация промо, награды, provisioned: доступ готов, GET /happ, GET /app-import/{app}, nav:connect (+36 more)

### Community 21 - "Код: internal/web"
Cohesion: 0.11
Nodes (41): TestAppearanceSaveRequiresOwnerCSRFAndValidTokens(), TestExperiencePagesRespectRoles(), TestUnpaidOrderCannotBeRetried(), sessionCookieFor(), TestAdminFormsRequireCSRF(), TestAdminIsClosedToOrdinaryUsers(), TestAdminIsOpenToOwner(), TestAdminIsOpenToStaffMember() (+33 more)

### Community 22 - "Развитие HTML: предложения"
Cohesion: 0.08
Nodes (43): Очередь проблем оплаты и выдачи, Приватное состояние chatData, Галочка как серверное действие, Чистый чат после сохранения, Выбор клиента и HTTPS-мостик, Нативное сообщение для копирования, Дополнительные страницы без новых прав, Development: отдельная БД и namespace (+35 more)

### Community 23 - "Веб-сайт и кабинет"
Cohesion: 0.12
Nodes (20): cardState, connectPlatform, PreviewScreen, 42 демонстрационных сценария / screenlab, quoteUsername(), telegramUsername(), calendarDate(), connectionPlatform() (+12 more)

### Community 24 - "Код: .github/workflows"
Cohesion: 0.05
Nodes (41): concurrency, cancel-in-progress, group, CGO_ENABLED, DATABASE_URL, POSTGRES_DB, POSTGRES_PASSWORD, POSTGRES_USER (+33 more)

### Community 25 - "Подтверждение оплаты и выдача"
Cohesion: 0.06
Nodes (35): Ожидать подтверждение сохранённого метода, Очередь проблем и повтор выдачи, Оплатить с внутреннего баланса, Разрешённые recurring-возможности, Серверный opt-in автопродления, /grant, Компенсация: 3 дня на обращение, Лимит компенсаций за 24 часа (+27 more)

### Community 26 - "Код: internal/bot"
Cohesion: 0.09
Nodes (32): richRequest, TestCleanSupportPreservesContentInHistoryBeforeDeleting(), TestDeviceDetailsReturnToTheirOriginalPage(), TestUniversalScreenFallsBackWhenRichUnsupported(), TestBerlogaLegacyUsesBrandedRichMainAndSubscription(), TestOrdinaryMainUsesDesignedRichDocumentAndNoWidget(), TestConnectionSurvivesDeviceListFailure(), TestLegacyConfiguredStartDoesNotCallWidgetAPI() (+24 more)

### Community 27 - "Код: deploy/development"
Cohesion: 0.07
Nodes (37): depends_on, env_file, healthcheck, image, logging, ports, restart, volumes (+29 more)

### Community 28 - "Покупка, триал и промокоды"
Cohesion: 0.09
Nodes (37): Атомарная покупка с баланса, Компенсация 3 дня на обращение, Атомарная фиксация компенсации, Устойчивый заказ триала и подарка, Журнал идемпотентных шагов выдачи, Поздняя подтверждённая оплата, Изолированный renderer legacy, Аренда доставки уведомления (+29 more)

### Community 29 - "Поддержка и история переписки"
Cohesion: 0.11
Nodes (14): Фото / документ и подпись, Убрать сохранённое входящее сообщение, /close, /tickets, Записать сообщение и вложение, Ответить и продолжать диалог, Пользователь: проблема решена, История обращения (+6 more)

### Community 30 - "Сохранённое состояние и миграции"
Cohesion: 0.07
Nodes (9): DeviceRenewal, orders и payment inbox, Order, scanOrder(), reservePromoOrder(), FunnelRow, WebSupportRequest, adminDashboard (+1 more)

### Community 31 - "Код: internal/web"
Cohesion: 0.10
Nodes (25): TestBrokenBlocksFailStart(), TestRenderLandingWithCustomBlocks(), TestThemeBlocksReplaceBuiltIn(), TestThemeBuildsDefaultBlocks(), buildBlocks(), cssVariables(), dateText(), daysWord() (+17 more)

### Community 32 - "Код: docker-compose.yml"
Cohesion: 0.07
Nodes (29): build, depends_on, env_file, environment, ports, restart, volumes, condition (+21 more)

### Community 33 - "Код: internal/bot/texts"
Cohesion: 0.12
Nodes (21): textsPtr(), TestExpiredMenuUsesInactiveLayout(), containsString(), menuState(), Default(), Load(), mergeMenu(), DefaultMenuButtons() (+13 more)

### Community 34 - "Код: internal/web"
Cohesion: 0.09
Nodes (12): hashToken(), newOAuthStates(), normalizeCode(), randomBytes(), randomToken(), clientIP(), localPath(), newRateLimiter() (+4 more)

### Community 35 - "Код: internal/web"
Cohesion: 0.15
Nodes (8): NormalizePromoCode(), TestNormalizePromoCode(), deviceError(), promoError(), telegramIDOf(), panelUsername(), purchaseError(), trialError()

### Community 36 - "Сотрудники и операции"
Cohesion: 0.11
Nodes (29): Партнёрский кабинет и награды, Подтверждение приглашения партнёра, Пульт Telegram и веб-админка, /partners, /polls, Партнёры, промокоды и приглашения, Опросы: вопрос → варианты → ответы, Рабочий стол сотрудника (+21 more)

### Community 37 - "Код: internal/payments"
Cohesion: 0.09
Nodes (25): every(), TestBackgroundTaskSurvivesFailureAndStopsOnCancellation(), TestReconcileProviderConfirmsAmountAndIdentity(), newAPIClient(), NewPlategaProvider(), TestPlategaSubscriptionActivationAloneDoesNotGrantPaidAccess(), TestYooKassaTrustsVerifiedPaymentInsteadOfWebhookClaims(), TestPlategaChecksCredentials() (+17 more)

### Community 39 - "Код: internal/web"
Cohesion: 0.12
Nodes (7): constantTimeEqual(), newLiveHub(), TestLoggingPreservesStreamingFlush(), panelEvent(), TestPanelEventParsingIsDefensive(), liveHub, Server

### Community 40 - "Код: internal/web/static"
Cohesion: 0.09
Nodes (16): assert, fs, initial, source, {test}, vm, assert, fs (+8 more)

### Community 41 - "Код: internal/config"
Cohesion: 0.18
Nodes (26): TestBotStyleAndRecurringDefaults(), TestHTMLTemplateCannotEscapeItsDirectory(), contains(), fallback(), Load(), ParseBytes(), baseEnv(), clearEnv() (+18 more)

### Community 42 - "HTML: карточка и проверка действий"
Cohesion: 0.09
Nodes (21): Шаблон, тема и SHA256 документа, bot_cards / bot_views / checkout preference, BOT_STYLE и настройки, deliverUI: создать или изменить, Изменить приватный chatData, BOT_STYLE=html → cards, BOT_STYLE=legacy → classic, Markdown поддержки → rich / HTML chunks (+13 more)

### Community 43 - "Код: internal/web"
Cohesion: 0.13
Nodes (6): knownStaffAction(), RoleCan(), orderBelongsTo(), ticketOwned(), firstRunes(), adminSession

### Community 44 - "Код: internal/bot/texts"
Cohesion: 0.16
Nodes (24): Texts, ButtonTexts, BuyTexts, CommonTexts, ConnectTexts, DevicesTexts, HelpTexts, HistoryTexts (+16 more)

### Community 45 - "Код: internal/support"
Cohesion: 0.14
Nodes (9): loadDotEnv(), TestLoadDotEnv(), TestLoadDotEnvMissingFile(), LoadKnowledge(), TestKnowledgeAppendsToPrompt(), TestKnowledgeEmptyWithoutFiles(), TestKnowledgeReadsMainAndExtraFiles(), reservedPagePath() (+1 more)

### Community 46 - "Код: internal/appearance"
Cohesion: 0.17
Nodes (16): WidgetPreviewAssets, export(), main(), contrast(), Default(), Theme, Load(), Parse() (+8 more)

### Community 47 - "Код: internal/botui/legacy/transport"
Cohesion: 0.18
Nodes (13): IsNotModified(), IsUnsupported(), messagePayload(), New(), newServer(), TestEditIgnoresNotModified(), TestEditPutsMessageID(), TestSendPutsRichMessage() (+5 more)

### Community 48 - "Код: internal/domain"
Cohesion: 0.14
Nodes (8): CustomerGuidance, GuideCustomer(), TestCustomerGuidancePrioritizesPaidIssuanceOverAnotherPurchase(), TestCustomerGuidanceStates(), RecurringAgreement, TestRecurringCustomerStates(), scanAgreement(), adminOrdersData

### Community 49 - "Поддержка и история переписки"
Cohesion: 0.13
Nodes (18): Передача зависшей AI-поддержки человеку, Эвристическая фильтрация AI-контекста, Каталог тарифов и устройств, Пример базы знаний помощника, Пример обещает продление слотов, Обращение по проверенному заказу, Рестарт сохраняет историю, не режим ввода, Закрытие без потери истории (+10 more)

### Community 50 - "Сохранённое состояние и миграции"
Cohesion: 0.14
Nodes (7): support_tickets и support_messages, Message, Ticket, scanTicket(), adminTickets, supportData, ticketData

### Community 51 - "HTML: карточка и проверка действий"
Cohesion: 0.24
Nodes (19): createVividWidget и chatDataChanged, Пустое подтверждение не завершает обновление, Действие карточки по индексу, Ожидание подтверждения и нового состояния, chatDataChanged применяет серверный экран, Согласие на автопродление подтверждает сервер, Ошибка и таймаут возвращают возможность повторить, Четыре вкладки HTML-кабинета (+11 more)

### Community 52 - "Сотрудники и операции"
Cohesion: 0.12
Nodes (18): Рассылка: черновик и аудитория, Доставка с повторной проверкой роли, Предпросмотр и подтверждение рассылки, Неоднозначная доставка без автоповтора, Первое подключение в воронке, Устойчивое уведомление нового устройства, Подписанное событие Remnawave, Единая матрица полномочий (+10 more)

### Community 53 - "Код: internal/bot"
Cohesion: 0.15
Nodes (10): menuConditions, ticketConversation, ticketThreadPage, richAttr(), richButton(), TestRichButtonEscapesAttributes(), ticketTextParts(), telegramURL() (+2 more)

### Community 54 - "Код: internal/payments"
Cohesion: 0.12
Nodes (8): PaymentOption, PlanDevicesTotal(), TestPlanDevicesTotal(), daysWord(), methodLabel(), providerLabel(), buyData, buyQuote

### Community 55 - "Код: internal/web"
Cohesion: 0.20
Nodes (6): RewardValueText(), TestRewardValueText(), datetimeText(), firstLines(), templateFuncs(), renderer

### Community 56 - "Код: internal/storage"
Cohesion: 0.25
Nodes (4): Partner, getPartner(), linkPartnerSubject(), scanPartner()

### Community 57 - "Код: sqlc.yaml"
Cohesion: 0.14
Nodes (13): go, emit_empty_slices, emit_interface, emit_json_tags, out, package, sql_package, sql (+5 more)

### Community 58 - "Код: internal/bot"
Cohesion: 0.23
Nodes (6): subscriptionWidgetData, TestSubscriptionWidgetQueryRejectsOtherContexts(), TestSubscriptionWidgetRejectsForeignQueryBeforeReadingDatabase(), isWidgetUnsupported(), privateSubscriptionWidgetQuery(), widgetDataForAccess()

### Community 60 - "Код: internal/bot"
Cohesion: 0.20
Nodes (7): TestUniversalScreenPreservesCodeNewlines(), screenRichHTML(), splitScreenBlocks(), cardHTML(), TestCardRejectsOversizeContent(), MascotAsset(), previewMascot()

### Community 61 - "Код: internal/bot"
Cohesion: 0.31
Nodes (4): failureCaller, refusedWidgetCaller, scriptedCaller, viewCaller

### Community 62 - "Legacy: доставка и восстановление"
Cohesion: 0.20
Nodes (10): Удаление обработанной команды, Компактный native-экран, Rich main не использует CustomerGuidance, Telegram HTML и inline-кнопки, Обычный текст при ошибке разметки, Сначала сохранить ID экрана, Убрать старый интерфейс после смены режима, Rich API недоступен (+2 more)

### Community 63 - "Код: internal/support"
Cohesion: 0.22
Nodes (4): orderStatusText(), Facts, FactsProvider, privacyTestFacts

### Community 64 - "Код: internal/web"
Cohesion: 0.29
Nodes (4): captureSender, captureNotifier, attachmentNotifier, Notifier

### Community 65 - "Код: internal/botui/legacy"
Cohesion: 0.36
Nodes (7): Document(), Fact(), TestDocumentEscapesEveryUserControlledField(), TestDocumentKeepsConnectionStepsAndWarningsReadable(), TestEmptyDocumentNeverInventsSubscriptionFacts(), Page, Stat

### Community 66 - "Код: internal/web/static"
Cohesion: 0.47
Nodes (8): actionTarget(), choose(), colorVars(), feedback(), remember(), render(), selectedModel(), themeCSS()

### Community 67 - "Устройства и дополнительные места"
Cohesion: 0.25
Nodes (8): Заказ устройств / продление grant, Цена устройств до окончания подписки, Экран счёта и внешняя оплата, /pay/success и /pay/fail, pending: ожидаем оплату, GET /pay/success, GET /pay/fail, Дополнительные места и продление

### Community 68 - "Код: internal/domain"
Cohesion: 0.33
Nodes (6): ParseMoney(), TestParseMoney(), TestParseMoneyNegative(), TestParseMoneyRejectsGarbage(), RewardValueFromInput(), TestRewardValueFromInput()

### Community 69 - "Код: internal/llm"
Cohesion: 0.40
Nodes (4): errorText(), chatRequest, chatResponse, Message

### Community 70 - "Код: internal/web"
Cohesion: 0.50
Nodes (3): importDeepLink(), TestImportBridgeKeepsFixedSchemesAndEscapesURL(), isHTTPURL()

### Community 71 - "Код: internal/web"
Cohesion: 0.50
Nodes (4): signPanel(), TestPanelSignatureUsesRawBody(), TestPanelWebhookAuthenticationAndDurableReplay(), verifyPanelSignature()

## Ambiguous Edges - Review These
- `Задокументированные пробелы рабочих операций` → `Сохранённая история обращения`  [AMBIGUOUS]
  docs/python-go-practical-review.md · relation: документировал прежний разрыв

## Knowledge Gaps
- **118 isolated node(s):** `name`, `push`, `pull_request`, `workflow_dispatch`, `contents` (+113 more)
  These have ≤1 connection - possible missing edges. (Counts symbols only; 295 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **13 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `Выдать paid-заказ по типу` connect `Подтверждение оплаты и выдача` to `Код: internal/payments`?**
  _High betweenness centrality (0.007) - this node is a cross-community bridge._
- **What connects `name`, `push`, `pull_request` to the rest of the system?**
  _118 weakly-connected nodes found - possible missing edges._
- **Should `Код: internal/storage` be split into smaller, more focused modules?**
  _Cohesion score 0.017733411626541396 - nodes in this community are weakly interconnected._
- **Why does `Блокировка subject и повторное чтение` connect `Подтверждение оплаты и выдача` to `Подтверждение оплаты и выдача`, `Код: internal/payments`?**
  _High betweenness centrality (0.007) - this node is a cross-community bridge._
- **Should `Код: internal/storage` be split into smaller, more focused modules?**
  _Cohesion score 0.03329935440027183 - nodes in this community are weakly interconnected._
- **Should `Код: internal/remnawave` be split into smaller, more focused modules?**
  _Cohesion score 0.044182621502209134 - nodes in this community are weakly interconnected._

### Low-confidence Hints
_AMBIGUOUS edges — the extractor was unsure. Verify before acting on these._

- **What is the exact relationship between `Задокументированные пробелы рабочих операций` and `Сохранённая история обращения`?**
  _Edge tagged AMBIGUOUS (relation: документировал прежний разрыв) - confidence is low._