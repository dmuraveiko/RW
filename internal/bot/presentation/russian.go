package presentation

import "github.com/dmuraveiko/RW/internal/bot/app"

type Russian struct{}

func (Russian) Text(reply app.Reply) string {
	switch reply {
	case app.ReplyNone:
		return ""
	case app.ReplyStart:
		return "Бот RealWallet на связи. Для начала работы отправьте действующий инвайт."
	case app.ReplyStartInvite:
		return "Инвайт получен. Выполняется привязка сессии."
	case app.ReplyHelp:
		return "Доступные команды:\n/start — начать работу\n/status — проверить состояние бота\n/help — показать эту справку"
	case app.ReplyStatus:
		return "Бот работает и подключён к Telegram API."
	case app.ReplyTextUnsupported:
		return "Используйте /start, /status или /help."
	default:
		return "Неизвестная команда. Используйте /help."
	}
}
