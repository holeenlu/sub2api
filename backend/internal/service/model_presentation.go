package service

import "strings"

// ModelPresentation describes client display only. It never grants account
// supply, group access, pricing, or permission to bypass request admission.
func ModelPresentation(id, visibility, purpose string) (string, string) {
	name := strings.ToLower(strings.TrimSpace(id))
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	if strings.HasPrefix(name, codexAutoModelPrefix) || name == "gpt-reserve" {
		purpose = "background"
	}
	if purpose != "background" {
		purpose = ""
	}
	if strings.EqualFold(strings.TrimSpace(visibility), "hide") || purpose == "background" {
		return "hide", purpose
	}
	if strings.EqualFold(strings.TrimSpace(visibility), "list") {
		return "list", purpose
	}
	return "", purpose
}

func isBackgroundCodexModel(id string) bool {
	_, purpose := ModelPresentation(id, "", "")
	return purpose == "background"
}
