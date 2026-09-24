//go:build faults

package faultcheck

import "github.com/bwmarrin/discordgo"

const defaultFieldValue = "1"

type Field struct {
	CustomID string
	Value    string
	IsSelect bool
}

type Target struct {
	CustomID string
	Values   []string
	Fields   []Field
	IsModal  bool
}

func Targets(payload map[string]any) []Target {
	body := payload
	if data, isCallback := payload["data"].(map[string]any); isCallback {
		body = data
	}

	if responseType, _ := payload["type"].(float64); discordgo.InteractionResponseType(responseType) == discordgo.InteractionResponseModal {
		modal := Target{CustomID: stringField(body, "custom_id"), IsModal: true}
		walkComponents(body["components"], func(component map[string]any) {
			field := Field{CustomID: stringField(component, "custom_id"), Value: defaultFieldValue}
			if values := optionValues(component); len(values) > 0 {
				field.Value, field.IsSelect = values[0], true
			}
			modal.Fields = append(modal.Fields, field)
		})
		return []Target{modal}
	}

	var targets []Target
	walkComponents(body["components"], func(component map[string]any) {
		targets = append(targets, Target{CustomID: stringField(component, "custom_id"), Values: optionValues(component)})
	})
	return targets
}

func walkComponents(node any, visit func(component map[string]any)) {
	switch typed := node.(type) {
	case []any:
		for _, child := range typed {
			walkComponents(child, visit)
		}
	case map[string]any:
		if stringField(typed, "custom_id") != "" {
			visit(typed)
		}
		walkComponents(typed["components"], visit)
		walkComponents(typed["component"], visit)
	}
}

func optionValues(component map[string]any) []string {
	options, _ := component["options"].([]any)
	values := make([]string, 0, len(options))
	for _, option := range options {
		if entry, isObject := option.(map[string]any); isObject {
			if value := stringField(entry, "value"); value != "" {
				values = append(values, value)
			}
		}
	}
	return values
}

func stringField(object map[string]any, key string) string {
	text, _ := object[key].(string)
	return text
}
