package bot

import (
	"encoding/json"
	"strings"

	"github.com/mymmrac/telego"
	"github.com/toonbr1me/vivid-go-bot/internal/config"
)

// PreviewScreen is an isolated demonstration. It never calls payments, a
// panel, or a real chat. The same component model and renderers draw it.
type PreviewScreen struct {
	ID          string    `json:"id"`
	Group       string    `json:"group"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	State       cardState `json:"state"`
	RichHTML    string    `json:"rich_html"`
	Targets     []string  `json:"targets"`
}

func ScreenCatalog() []PreviewScreen {
	b := &Bot{cfg: &config.Settings{BrandName: "Vivid"}}
	var screens []PreviewScreen
	add := func(id, group, title, description string, state cardState) {
		prepared, err := prepareCard(state)
		if err != nil {
			return
		}
		for i := range prepared.Pages {
			prepared.Pages[i].Revision = "preview"
		}
		rich, kb := richCardDocument(state)
		if rows, ok := richButtonRows(kb); ok {
			rich += "\n" + rows
		}
		targets := []string{}
		for _, action := range state.Actions {
			target := PreviewTarget(action.CallbackData)
			if action.CopyText != nil {
				target = "copy"
			}
			targets = append(targets, target)
		}
		screens = append(screens, PreviewScreen{ID: id, Group: group, Title: title, Description: description, State: prepared, RichHTML: rich, Targets: targets})
	}
	makeState := func(page cardPage, actions ...telego.InlineKeyboardButton) cardState {
		return cardWithButtons(page, actions)
	}
	a := button
	add("mode", "Начало", "Режим сервиса", "В рабочем боте режим задаётся через BOT_STYLE", makeState(cardPage{Title: "Вид бота", Value: "Как удобнее?", Note: "Для HTML-карточек нужен обновлённый Telegram"}, a("Обычный · rich", "preview:rich"), a("Новый · HTML", "preview:home")))
	add("home", "Начало", "Активная подписка", "Срок, устройства и одно главное действие", cardUtility(makeState(cardPage{Title: "Vivid", Hero: true, Used: 2, Limit: 5, Tab: "subscription", Status: "Активна", Tone: "ok", Kicker: "Оплачено до", Value: "13 декабря 2026", Note: "Осталось 30 дней", Stats: []cardStat{{Label: "Устройства", Value: "2 / 5", Nav: "devices"}, {Label: "Использовано", Value: "18.4 ГБ"}}}, a("Подключить устройство", "nav:connect"), a("Продлить подписку", "nav:buy")), "nav:subscription", true))
	add("empty", "Начало", "Нет подписки", "Пробный доступ и тарифы без пустого меню", makeState(cardPage{Title: "Vivid", Tab: "subscription", Value: "Попробуйте подключение", Note: "Начните с пробного доступа или выберите срок"}, a("Попробовать бесплатно", "preview:trial"), a("Выбрать подписку", "nav:buy")))
	add("expired", "Начало", "Подписка истекла", "Восстановление доступа с понятным следующим шагом", makeState(cardPage{Title: "Vivid", Tab: "subscription", Status: "Истекла", Tone: "warn", Value: "Верните доступ", Note: "Настройки приложения сохраняются"}, a("Продлить подписку", "nav:buy"), a("Нужна помощь", "nav:support")))
	add("trial", "Начало", "Пробный доступ", "Выданный доступ ведёт сразу к подключению", makeState(cardPage{Title: "Пробный доступ", Tab: "subscription", Status: "Активен", Tone: "ok", Value: "Можно подключаться", Note: "Демонстрационный срок: 3 дня"}, a("Подключить устройство", "nav:connect"), a("Посмотреть тарифы", "nav:buy")))
	add("plans", "Покупка", "Тарифы", "Срок, цена и число устройств до выбора оплаты", makeState(cardPage{Title: "Подписка", Tab: "subscription", Value: "Выберите срок", Note: "Демонстрационные цены · текущий оплаченный срок сохранится", Layout: "plans"}, a("1 месяц · 299 ₽", "preview:plan_manual"), a("3 месяца · 799 ₽", "preview:plan_manual"), a("12 месяцев · 2 499 ₽", "preview:plan_manual"), a("Есть промокод", "nav:promo")))
	add("plan", "Покупка", "Подтверждение тарифа", "Проверка состава покупки перед созданием счёта", cardUtility(makeState(cardPage{Title: "Продление", Tab: "subscription", Value: "799 ₽", Note: "Добавится к текущей подписке", Stats: []cardStat{{Label: "Срок", Value: "90 дней"}, {Label: "Устройства", Value: "3"}}}, a("Банковская карта", "preview:payment"), a("СБП", "preview:payment"), a("Оплатить с баланса", "preview:paid")), "nav:buy", false))
	falseValue, trueValue := false, true
	for _, variant := range []struct {
		id      string
		checked *bool
		title   string
	}{{"plan_manual", &falseValue, "Разовая оплата"}, {"plan_auto", &trueValue, "Автопродление"}} {
		page := cardPage{Title: "Оформление подписки", Tab: "subscription", Kicker: "90 дней · 3 устройства", Value: "799 ₽", Note: "Оплаченный срок сохранится", Layout: "checkout"}
		state := cardState{Pages: []cardPage{page}}
		target := "preview:plan_auto"
		if *variant.checked {
			target = "preview:plan_manual"
		}
		actions := []telego.InlineKeyboardButton{a("Продлевать автоматически?", target), a("ЮKassa · Банковская карта", "preview:payment"), a("Platega · СБП", "preview:payment")}
		if !*variant.checked {
			actions = append(actions, a("WATA · Банковская карта", "preview:payment"), a("Оплатить с баланса", "preview:paid"))
		}
		state.Actions = actions
		for i, action := range actions {
			style := ""
			var checked *bool
			detail := ""
			if i == 0 {
				style = "checkbox"
				checked = variant.checked
				detail = "Выключено · разовая оплата"
				if *checked {
					detail = "799 ₽ каждые 90 дней · отмена в аккаунте"
				}
			} else if i == 1 {
				style = "primary"
			}
			state.Pages[0].Buttons = append(state.Pages[0].Buttons, cardButton{Text: action.Text, Index: i, Style: style, Checked: checked, Detail: detail})
		}
		add(variant.id, "Покупка", variant.title, "Галочка фильтрует способы оплаты по поддержке автоплатежей", cardUtility(state, "nav:buy", false))
	}
	add("payment", "Покупка", "Ожидание оплаты", "Открытие счёта и проверка статуса без повторной покупки", makeState(cardPage{Title: "Заказ", Tab: "subscription", Status: "Ожидает оплаты", Value: "799 ₽", Note: "Заказ D-1042 · продление на 90 дней"}, a("Перейти к оплате", "preview:paid"), a("Я оплатил · проверить", "preview:provisioning"), a("История покупок", "nav:history")))
	add("provisioning", "Покупка", "Выдача доступа", "Оплата получена, выдача ещё выполняется", makeState(cardPage{Title: "Заказ D-1042", Tab: "subscription", Status: "Оплачен", Tone: "ok", Value: "Готовим доступ", Body: "Оплата получена\nВыдаём подписку\nСсылка появится после завершения"}, a("Проверить статус", "preview:paid"), a("Помощь по заказу", "preview:ticket")))
	add("paid", "Покупка", "Успешная покупка", "Новый срок и переход к подключению", makeState(cardPage{Title: "Подписка продлена", Tab: "subscription", Status: "Готово", Tone: "ok", Kicker: "Доступ до", Value: "13 марта 2027", Note: "К оплаченному сроку добавлено 90 дней"}, a("Подключиться", "nav:connect"), a("Моя подписка", "nav:subscription")))
	add("payment_failed", "Покупка", "Счёт истёк", "Понятный повтор оплаты без утверждения о списании", makeState(cardPage{Title: "Заказ", Tab: "subscription", Status: "Счёт истёк", Tone: "warn", Value: "Создайте новый счёт", Note: "Статус предыдущей оплаты доступен в истории"}, a("Выбрать способ оплаты", "preview:plan"), a("История покупок", "nav:history")))
	add("connect", "Подключение", "Выбор устройства", "Подходящая инструкция вместо списка всех магазинов", b.connectPickerCard())
	add("install", "Подключение", "Установка приложения", "Первый шаг для выбранной платформы", b.platformCard("android"))
	add("import", "Подключение", "Добавление подписки", "Надёжное получение ссылки и штатное копирование", cardUtility(makeState(cardPage{Title: "Android", Tab: "subscription", Value: "Добавьте подписку", Note: "Получите ссылку и импортируйте её в Happ", Step: 2}, copyButton("Получить ссылку", "https://example.com/demo-subscription"), a("Открыть страницу подписки", "preview:copy"), a("Проверить готовность", "preview:diagnostics")), "card:platform:android", false))
	add("copy", "Подключение", "Ссылка для копирования", "Отдельное обычное сообщение: ограничение защищённых виджетов", makeState(cardPage{Title: "Ссылка для подключения", Tab: "subscription", Value: "Штатное копирование", Note: "В рабочем чате бот отправит сообщение с кнопкой копирования"}, a("Скопировать демо-ссылку", "preview:copied"), a("Вернуться к подключению", "preview:import")))
	add("copied", "Подключение", "После копирования", "Следующее действие без ложной проверки соединения", makeState(cardPage{Title: "Подключение", Tab: "subscription", Value: "Добавьте ссылку в Happ", Note: "Импортируйте подписку и включите подключение в приложении"}, a("Проверить подписку", "preview:diagnostics"), a("Не получается", "nav:support")))
	add("diagnostics", "Подключение", "Проверка готовности", "Проверка подписки, ссылки и лимита; это не тест сети телефона", makeState(cardPage{Title: "Проверка подключения", Tab: "support", Value: "Всё готово к настройке", Body: "Подписка активна\nСсылка доступна\nЕсть свободное место", Status: "3 проверки", Tone: "ok"}, a("Перейти к подключению", "nav:connect"), a("Помощь специалиста", "preview:compose")))
	add("devices", "Устройства", "Список устройств", "Компактные записи с платформой и коротким идентификатором", makeState(cardPage{Title: "Устройства", Tab: "devices", Note: "2 подключено · 1 место свободно"}, a("iPhone · iOS · A91F", "preview:device"), a("MacBook · macOS · B204", "preview:device"), a("Подключить ещё", "nav:connect"), a("Больше мест", "preview:slots")))
	add("device", "Устройства", "Устройство", "Сведения и отдельное действие отвязки", cardUtility(makeState(cardPage{Title: "Устройство", Tab: "devices", Value: "MacBook", Note: "macOS · B204", Body: "Отвязка удаляет запись. При следующем подключении устройство может появиться снова."}, telego.InlineKeyboardButton{Text: "Отвязать устройство", CallbackData: "preview:detach", Style: "danger"}), "nav:devices", false))
	add("detach", "Устройства", "Подтверждение отвязки", "Последствия видны до выполнения действия", cardUtility(makeState(cardPage{Title: "Подтверждение", Tab: "devices", Value: "Отвязать MacBook?", Note: "Подписка сохранится"}, telego.InlineKeyboardButton{Text: "Да, отвязать", CallbackData: "preview:devices", Style: "danger"}, a("Оставить подключённым", "preview:device")), "preview:device", false))
	add("slots", "Устройства", "Дополнительные места", "Количество, период и итоговая цена отдельно от списка устройств", makeState(cardPage{Title: "Дополнительные места", Tab: "devices", Value: "До конца подписки", Note: "Демонстрационный расчёт на оставшиеся 18 дней"}, a("+1 устройство · 36 ₽", "preview:slot_plan"), a("+3 устройства · 84 ₽", "preview:slot_plan"), a("Как считается цена", "preview:slot_math")))
	add("slot_math", "Устройства", "Расчёт стоимости", "Понятное объяснение доплаты без скрытого срока", makeState(cardPage{Title: "Расчёт доплаты", Tab: "devices", Value: "36 ₽", Body: "Демо: 60 ₽ за 30 дней\nОсталось 18 дней\n60 × 18 / 30 = 36 ₽"}, a("Выбрать пакет", "preview:slots")))
	add("slot_plan", "Устройства", "Оплата мест", "Подтверждение количества и срока", cardUtility(makeState(cardPage{Title: "Дополнительное место", Tab: "devices", Value: "36 ₽", Note: "+1 устройство до конца текущей подписки"}, a("Выбрать оплату", "preview:payment")), "preview:slots", false))
	add("profile", "Аккаунт", "Профиль и баланс", "Деньги, история и управление аккаунтом", makeState(cardPage{Title: "Аккаунт", Tab: "more", Kicker: "Баланс", Value: "250 ₽", Note: "Демонстрационные данные"}, a("История покупок", "nav:history"), a("Промокод", "nav:promo"), a("Настройки", "nav:settings")))
	add("history", "Аккаунт", "История покупок", "Статусы, сроки и восстановление незавершённого заказа", makeState(cardPage{Title: "Покупки", Tab: "more", Note: "Последние заказы"}, a("D-1042 · 799 ₽ · оплачено", "preview:provisioning"), a("D-0987 · 299 ₽ · доступ выдан", "preview:paid"), a("D-0950 · счёт истёк", "preview:payment_failed")))
	add("promo", "Аккаунт", "Промокод", "Ввод в обычном чате и пересчёт предложения", makeState(cardPage{Title: "Промокод", Tab: "more", Value: "Есть код?", Note: "Пришлите его боту одним сообщением"}, a("Показать применение", "preview:promo_applied"), a("Вернуться к тарифам", "nav:buy")))
	add("promo_applied", "Аккаунт", "Применённый промокод", "Размер скидки виден до оплаты", makeState(cardPage{Title: "Промокод применён", Tab: "subscription", Value: "Скидка 10%", Note: "Демо-код START · окончательная цена в тарифах"}, a("Перейти к тарифам", "nav:buy"), a("Убрать промокод", "nav:promo")))
	add("referral", "Аккаунт", "Приглашения", "Условия награды и результат приглашений", makeState(cardPage{Title: "Пригласить друга", Tab: "more", Value: "Пользуйтесь вместе", Note: "Награда зависит от настроек проекта", Stats: []cardStat{{Label: "Приглашено", Value: "4"}, {Label: "Награда", Value: "7 дней"}}}, a("Получить ссылку", "preview:copy"), a("Условия программы", "preview:referral_terms")))
	add("referral_terms", "Аккаунт", "Условия приглашений", "Настраиваемые правила без обещаний несуществующей награды", makeState(cardPage{Title: "Программа приглашений", Tab: "more", Body: "Награда начисляется за подтверждённую покупку приглашённого пользователя. Вид награды задаёт владелец проекта."}, a("Назад", "nav:referral")))
	add("settings", "Аккаунт", "Настройки уведомлений", "Управление напоминаниями без лишнего меню", makeState(cardPage{Title: "Уведомления", Tab: "more", Note: "Выберите, о чём напоминать"}, a("Об окончании · включены", "preview:settings_off"), a("О незавершённой оплате · включены", "preview:settings_off")))
	add("settings_off", "Аккаунт", "Выключенное уведомление", "Состояние меняется на том же экране", makeState(cardPage{Title: "Уведомления", Tab: "more", Note: "Настройки сохранены в демонстрации"}, a("Об окончании · выключены", "nav:settings"), a("О незавершённой оплате · включены", "nav:settings")))
	add("support", "Помощь", "Поддержка", "Самопроверка, подключение и обращения", makeState(cardPage{Title: "Чем помочь?", Tab: "support", Value: "Разберёмся с подключением"}, a("Проверить подписку и устройства", "preview:diagnostics"), a("Инструкция подключения", "nav:connect"), a("Написать специалисту", "preview:compose"), a("Мои обращения", "preview:tickets")))
	add("compose", "Помощь", "Новое обращение", "Текст и вложения отправляются обычным сообщением в чат", makeState(cardPage{Title: "Новое обращение", Tab: "support", Value: "Опишите проблему", Note: "Что происходит и что уже пробовали? Можно приложить скриншот."}, a("Показать созданное обращение", "preview:ticket"), a("Отмена", "nav:support")))
	add("tickets", "Помощь", "Список обращений", "Возврат к переписке без создания дублей", makeState(cardPage{Title: "Мои обращения", Tab: "support"}, a("#104 · подключение · открыт", "preview:ticket"), a("#098 · оплата · решено", "preview:ticket_closed")))
	add("ticket", "Помощь", "Переписка", "Статус обращения и продолжение диалога", makeState(cardPage{Title: "Обращение #104", Tab: "support", Status: "Открыто", Body: "Вы: приложение не подключается\nПоддержка: проверьте, обновлена ли подписка в Happ"}, a("Ответить", "preview:compose"), a("Мои обращения", "preview:tickets")))
	add("ticket_closed", "Помощь", "Решённое обращение", "История остаётся доступной", makeState(cardPage{Title: "Обращение #098", Tab: "support", Status: "Решено", Tone: "ok", Body: "Подписка выдана после подтверждения оплаты"}, a("Новое обращение", "preview:compose"), a("К списку", "preview:tickets")))
	add("unavailable", "Состояния", "Панель недоступна", "Ошибка не превращается в ложное сообщение об истёкшей подписке", makeState(cardPage{Title: "Vivid", Tab: "subscription", Status: "Данные неполные", Tone: "warn", Value: "Проверим позже", Note: "Не удалось получить часть данных. Оплаченный срок не меняется."}, a("Обновить", "nav:subscription"), a("Поддержка", "nav:support")))
	add("more", "Аккаунт", "Все разделы", "Профиль, настройки, приглашения и сведения о сервисе", makeState(cardPage{Title: "Аккаунт", Tab: "more", Layout: "menu"}, a("Профиль и баланс", "nav:profile"), a("Пригласить друга", "nav:referral"), a("Уведомления", "nav:settings"), a("О сервисе", "preview:info")))
	add("info", "Аккаунт", "О сервисе", "Контакты и документы проекта", makeState(cardPage{Title: "О сервисе", Tab: "more", Value: "Vivid", Note: "Контакты, условия и помощь"}, a("Условия использования", "preview:documents"), a("Конфиденциальность", "preview:documents"), a("Поддержка", "nav:support")))
	add("documents", "Аккаунт", "Документы", "Юридические страницы открываются на сайте проекта", makeState(cardPage{Title: "Документы", Tab: "more", Note: "В рабочем боте кнопка откроет документ на сайте проекта"}, a("О сервисе", "preview:info")))
	return screens
}

func PreviewTarget(action string) string {
	if strings.HasPrefix(action, "preview:") {
		return strings.TrimPrefix(action, "preview:")
	}
	targets := map[string]string{"nav:main": "home", "nav:subscription": "home", "nav:buy": "plans", "nav:connect": "connect", "nav:devices": "devices", "nav:profile": "profile", "nav:history": "history", "nav:promo": "promo", "nav:settings": "settings", "nav:referral": "referral", "nav:support": "support", "nav:more": "more", "card:platform:android": "install", "card:platform:ios": "install", "card:platform:windows": "install", "card:platform:macos": "install", "card:import:android": "import", "card:clients:android": "install"}
	if target := targets[action]; target != "" {
		return target
	}
	return "home"
}

func CatalogJSON() ([]byte, error) { return json.Marshal(ScreenCatalog()) }
