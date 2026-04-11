package patterns

import (
	"strings"

	"github.com/mossagents/moss/kernel/model"
	"github.com/mossagents/moss/kernel/session"
)

func textUserMessage(text string) *model.Message {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	return &model.Message{
		Role:         model.RoleUser,
		ContentParts: []model.ContentPart{model.TextPart(text)},
	}
}

func eventText(event session.Event) string {
	if event.Content == nil {
		return ""
	}
	return model.ContentPartsToPlainText(event.Content.ContentParts)
}
