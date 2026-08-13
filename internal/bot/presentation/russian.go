package presentation

import "github.com/dmuraveiko/RW/internal/bot/app"

type Russian struct{}

func (Russian) Text(reply app.Reply) string {
	switch reply {
	case app.ReplyNone:
		return ""
	case app.ReplyStart:
		return "Бот RealWallet на связи.\n\nСейчас доступен демонстрационный режим. Привязка по инвайту и активация появятся в следующей версии.\n\nДоступные команды: /help и /status."
	case app.ReplyStartInvite:
		return "Бот RealWallet на связи. Инвайт получен, но привязка и активация появятся в следующей версии.\n\nИспользуйте /status, чтобы проверить соединение."
	case app.ReplyHelp:
		return "Доступные команды:\n/start — начать работу\n/status — проверить состояние бота\n/help — показать эту справку"
	case app.ReplyStatus:
		return "Бот работает и подключён к Telegram API. Бизнес-сценарии активации пока не включены."
	case app.ReplyTextUnsupported:
		return "Пока я понимаю только команды /start, /status и /help."
	default:
		return "Неизвестная команда. Используйте /help."
	}
}
