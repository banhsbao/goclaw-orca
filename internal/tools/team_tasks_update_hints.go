package tools

import (
	"fmt"
	"slices"
	"strings"
)

var nonUpdateTaskFields = []string{"text", "type", "task_type", "result", "status", "priority", "assignee"}

func ignoredUpdateFieldsHint(args map[string]any) string {
	var ignored []string
	for _, key := range nonUpdateTaskFields {
		if _, ok := args[key]; ok {
			ignored = append(ignored, key)
		}
	}
	if len(ignored) == 0 {
		return ""
	}
	slices.Sort(ignored)
	return fmt.Sprintf(". update ignores %s; to add or replace the brief, pass description=\"<full brief>\"", strings.Join(ignored, ", "))
}
