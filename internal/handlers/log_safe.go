package handlers

import (
	"strconv"
	"strings"
)

// SDL-738: в журнал бота уходят тип обновления, chat_id и команда — никогда
// текст сообщения, имя, username или полный объект обновления Telegram.

// describeUpdate — безопасная для журнала сводка обновления Telegram.
func describeUpdate(update map[string]interface{}) string {
	kind := "unknown"
	var chatID int64
	command := ""
	switch {
	case update["message"] != nil:
		kind = "message"
		if msg, ok := update["message"].(map[string]interface{}); ok {
			if chat, ok := msg["chat"].(map[string]interface{}); ok {
				if id, ok := chat["id"].(float64); ok {
					chatID = int64(id)
				}
			}
			if text, ok := msg["text"].(string); ok {
				command = commandOf(text)
			}
		}
	case update["callback_query"] != nil:
		kind = "callback_query"
		if cq, ok := update["callback_query"].(map[string]interface{}); ok {
			if msg, ok := cq["message"].(map[string]interface{}); ok {
				if chat, ok := msg["chat"].(map[string]interface{}); ok {
					if id, ok := chat["id"].(float64); ok {
						chatID = int64(id)
					}
				}
			}
		}
	default:
		for k := range update {
			if k != "update_id" {
				kind = k
				break
			}
		}
	}
	var b strings.Builder
	b.WriteString("type=")
	b.WriteString(kind)
	if chatID != 0 {
		b.WriteString(" chat_id=")
		b.WriteString(strconv.FormatInt(chatID, 10))
	}
	if command != "" {
		b.WriteString(" command=")
		b.WriteString(command)
	}
	return b.String()
}

// knownCommands — команды, которые маршрутизирует handleMessage. Только их
// имена попадают в журнал: произвольный текст на «/» — тоже текст
// пользователя и пишется как «other».
var knownCommands = map[string]bool{
	"/start": true, "/auth": true, "/menu": true, "/help": true, "/profile": true,
	"/settings": true, "/logout": true, "/users": true, "/stats": true, "/notifications": true,
}

// commandOf возвращает имя известной команды бота («/start») без аргументов —
// аргументы несут токены входа; для неизвестной команды — «other», для
// обычного текста — пустую строку.
func commandOf(text string) string {
	if !strings.HasPrefix(text, "/") {
		return ""
	}
	cmd := strings.Fields(text)
	if len(cmd) == 0 {
		return "other"
	}
	name := cmd[0]
	if i := strings.IndexByte(name, '@'); i > 0 {
		name = name[:i]
	}
	if !knownCommands[name] {
		return "other"
	}
	return name
}
