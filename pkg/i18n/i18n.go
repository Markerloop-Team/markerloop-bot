package i18n

// Locale текущая локаль (используется только для определения локали из Telegram)
type Locale string

const (
	LocaleRU Locale = "ru"
	LocaleEN Locale = "en"
	LocaleKK Locale = "kk"
)

// DefaultLocale локаль по умолчанию
const DefaultLocale = LocaleRU

// DetectLocale определяет локаль из language_code Telegram
// Используется для преобразования Telegram language_code в LanguageCode из go-sdk
func DetectLocale(languageCode string) Locale {
	switch languageCode {
	case "ru", "ru-RU":
		return LocaleRU
	case "en", "en-US", "en-GB":
		return LocaleEN
	case "kk", "kk-KZ":
		return LocaleKK
	default:
		return DefaultLocale
	}
}

// TelegramBotKeys ключи для сообщений Telegram бота (для предзагрузки)
var TelegramBotKeys = []string{
	// Общие
	"telegram.bot.start.welcome",
	"telegram.bot.start.message",
	"telegram.bot.start.registration_success",
	"telegram.bot.command.unknown",
	"telegram.bot.error.occurred",
	"telegram.bot.notification.title_format",

	// Авторизация
	"telegram.bot.button.auth",
	"telegram.bot.auth.success",
	"telegram.bot.auth.error",
	"telegram.bot.auth.already_linked",
	"telegram.bot.auth.token_invalid",
	"telegram.bot.auth.token_expired",
	"telegram.bot.auth.instructions",
	"telegram.bot.auth.required",
	"telegram.bot.auth.web_login_success",

	// Меню
	"telegram.bot.menu.main",
	"telegram.bot.button.profile",
	"telegram.bot.button.settings",
	"telegram.bot.button.logout",
	"telegram.bot.button.users",
	"telegram.bot.button.stats",
	"telegram.bot.button.notifications",
	"telegram.bot.button.open_site",
	"telegram.bot.button.back",

	// Профиль
	"telegram.bot.profile.title",
	"telegram.bot.profile.role",
	"telegram.bot.profile.registered",

	// Язык / Настройки
	"telegram.bot.language.choose",
	"telegram.bot.language.changed",

	// Выход
	"telegram.bot.logout.success",

	// Пользователи (superadmin)
	"telegram.bot.users.title",
	"telegram.bot.users.total",

	// Статистика (superadmin)
	"telegram.bot.stats.title",
	"telegram.bot.stats.total_users",
	"telegram.bot.stats.total_roles",
	"telegram.bot.stats.deploy_subscribers",

	// Уведомления (superadmin)
	"telegram.bot.notifications.title",
	"telegram.bot.notifications.enabled",
	"telegram.bot.notifications.disabled",
	"telegram.bot.notifications.description",
	"telegram.bot.notifications.enable",
	"telegram.bot.notifications.disable",
}
