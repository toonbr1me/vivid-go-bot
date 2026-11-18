package domain

// Connection clients are shared by Telegram and the web onboarding.
type ConnectionPlatform struct{ Key, Name, Store, URL string }

func ConnectionPlatforms() []ConnectionPlatform {
	return []ConnectionPlatform{
		{"ios", "iPhone / iPad", "App Store", "https://apps.apple.com/app/happ-proxy-utility/id6504287215"},
		{"android", "Android", "Google Play", "https://play.google.com/store/apps/details?id=com.happproxy"},
		{"windows", "Windows", "GitHub", "https://github.com/Happ-proxy/happ-desktop/releases/latest/download/setup-Happ.x64.exe"},
		{"macos", "macOS", "App Store", "https://apps.apple.com/app/happ-proxy-utility/id6504287215"},
	}
}
func ConnectionPlatformFor(key string) (ConnectionPlatform, bool) {
	for _, p := range ConnectionPlatforms() {
		if p.Key == key {
			return p, true
		}
	}
	return ConnectionPlatform{}, false
}

type SupportTopic struct{ Key, Label, Advice string }

func SupportTopics() []SupportTopic {
	return []SupportTopic{
		{"import", "Не добавляется подписка", "Установите приложение, затем повторите добавление. При ручном импорте скопируйте ссылку целиком."},
		{"connection", "Приложение не подключается", "Обновите подписку в приложении и попробуйте другую доступную локацию. Проверьте дату и время устройства."},
		{"sites", "Подключилось, но сайты не открываются", "Проверьте интернет без подключения и попробуйте другую доступную локацию в приложении."},
		{"payment", "Вопрос по оплате", "Проверьте заказ в истории покупок. Подтверждение оплаты и выдача доступа могут прийти в разное время."},
		{"other", "Другой вопрос", "Опишите, что произошло и что вы уже пробовали."},
	}
}
func SupportTopicFor(key string) SupportTopic {
	for _, t := range SupportTopics() {
		if t.Key == key {
			return t
		}
	}
	return SupportTopics()[len(SupportTopics())-1]
}
