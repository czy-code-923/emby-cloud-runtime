package api

import (
	"sort"
	"strings"
	"unicode/utf16"
)

// copy_text is a client-side action, not a callback. Keep the original keyboard
// and make the exact same value selectable in the message for older clients.
// Never put private copy values into callback_data, URLs, logs or shared state.
func memberCopyValues(rows [][]map[string]any) []string {
	values := []string{}
	seen := map[string]bool{}
	for _, row := range rows {
		for _, button := range row {
			value := ""
			switch copy := button["copy_text"].(type) {
			case map[string]string:
				value = copy["text"]
			case map[string]any:
				value, _ = copy["text"].(string)
			}
			if value != "" && !seen[value] && len([]rune(value)) <= 256 && len(values) < 20 {
				values = append(values, value)
				seen[value] = true
			}
		}
	}
	return values
}

func memberCopyText(text string, rows [][]map[string]any) string {
	for _, value := range memberCopyValues(rows) {
		if !strings.Contains(text, value) && len([]rune(text))+len([]rune(value))+1 <= 4096 {
			text += "\n" + value
		}
	}
	return text
}

func memberUTF16Len(text string) int {
	length := 0
	for _, r := range text {
		length += utf16.RuneLen(r)
	}
	return length
}

func memberCopyEntities(text string, rows [][]map[string]any) []telegramMessageEntity {
	values := memberCopyValues(rows)
	// Prefer complete URLs/passwords to a shorter account name inside them.
	sort.SliceStable(values, func(i, j int) bool { return len(values[i]) > len(values[j]) })
	entities := []telegramMessageEntity{}
	for _, value := range values {
		for start := 0; start < len(text); {
			index := strings.Index(text[start:], value)
			if index < 0 {
				break
			}
			index += start
			candidate := telegramMessageEntity{Type: "code", Offset: memberUTF16Len(text[:index]), Length: memberUTF16Len(value)}
			overlaps := false
			for _, existing := range entities {
				if candidate.Offset < existing.Offset+existing.Length && existing.Offset < candidate.Offset+candidate.Length {
					overlaps = true
					break
				}
			}
			if !overlaps {
				entities = append(entities, candidate)
				break
			}
			start = index + len(value)
		}
	}
	sort.Slice(entities, func(i, j int) bool { return entities[i].Offset < entities[j].Offset })
	return entities
}
