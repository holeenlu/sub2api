package basispoints

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// describeCatalog presents tool contracts as documentation, not native tool definitions.
func describeCatalog(catalog []any) string {
	var lines []string
	for _, raw := range catalog {
		// collectTools constructs every catalog entry as an object.
		entry, _ := raw.(object)
		line := "Client tool " + quoted(entry["name"]) + " (" + text(entry["type"]) + ")."
		if description := text(entry["description"]); description != "" {
			line += " " + description
		}
		if text(entry["type"]) == "custom" {
			line += " Set run_officejs summary to " + quoted("codex2api.custom/"+text(entry["name"])) + " and pass its exact raw text directly in code."
			if format := entry["format"]; format != nil {
				line += " Input format: " + quoted(format) + "."
			}
		} else {
			if supportsFunctionCodeTransport(text(entry["name"]), text(entry["type"]), entry["parameters"]) {
				line += " Use FUNCTION_CODE transport: set run_officejs summary to " + quoted(functionCodeTransportPrefix+text(entry["name"])) + ". Put the exact code argument directly in native code. Put all other supplied arguments in one JSON object in extended_summary, using only fields declared in the contract; use {} when there are none. Do not include code in that object."
			} else if supportsFunctionCmdTransport(text(entry["name"]), text(entry["type"]), entry["parameters"]) {
				line += " Use FUNCTION_CMD transport: set run_officejs summary to " + quoted(functionCmdTransportPrefix+text(entry["name"])) + ". Put the exact cmd argument directly in native code. Put all other supplied arguments in one JSON object in extended_summary, using only fields declared in the contract; use {} when there are none. Do not include cmd in that object."
			} else {
				line += " Pass a JSON object in the envelope's arguments field."
			}
			line += " Argument contract: " + describeSchema(entry["parameters"], 0)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n\n")
}

// describeCatalogExamples emits examples only for transports that are present
// in the current catalog and whose sample arguments satisfy that catalog's
// schema. Keeping examples data-driven prevents the relay prompt from
// teaching tools that the client did not actually declare.
func describeCatalogExamples(catalog []any, tools map[string]tool) string {
	const patch = "*** Begin Patch\n*** Add File: hello.js\n+console.log(\"hello\");\n*** End Patch"
	var examples []string
	for _, raw := range catalog {
		entry, _ := raw.(object)
		name, kind := text(entry["name"]), text(entry["type"])
		info, ok := tools[name]
		if !ok {
			continue
		}
		outer := object{"destructive": false, "references": []any{}}
		switch {
		case kind == "custom" && (name == "apply_patch" || strings.HasSuffix(name, ".apply_patch")):
			outer["summary"] = customTransportPrefix + name
			outer["extended_summary"] = "Relay an unchanged patch through the external client"
			outer["code"] = patch
		case kind == "function" && supportsFunctionCodeTransport(name, kind, entry["parameters"]):
			args := exampleToolArguments(info.Parameters, "code", "const value = 1;\nconsole.log(value);")
			if args == nil || info.Schema == nil || info.Schema.Validate(args) != nil {
				continue
			}
			metadata := cloneObject(args)
			delete(metadata, "code")
			outer["summary"] = functionCodeTransportPrefix + name
			outer["extended_summary"] = mustJSONText(metadata)
			outer["code"] = args["code"]
		case kind == "function" && supportsFunctionCmdTransport(name, kind, entry["parameters"]):
			args := exampleToolArguments(info.Parameters, "cmd", "printf '%s\\n' \\\"hello\\\"")
			if args == nil || info.Schema == nil || info.Schema.Validate(args) != nil {
				continue
			}
			metadata := cloneObject(args)
			delete(metadata, "cmd")
			outer["summary"] = functionCmdTransportPrefix + name
			outer["extended_summary"] = mustJSONText(metadata)
			outer["code"] = args["cmd"]
		case kind == "function" && (name == "apply_patch" || strings.HasSuffix(name, ".apply_patch")):
			args := exampleToolArguments(info.Parameters, "patch", patch)
			if args == nil || info.Schema == nil || info.Schema.Validate(args) != nil {
				continue
			}
			outer["summary"] = "Run client tool " + name
			outer["extended_summary"] = "Relay one client tool through the external client"
			outer["code"] = string(mustJSON(args))
		default:
			continue
		}
		outer["references"] = []any{name}
		encoded := mustJSON(outer)
		if len(encoded) > 8<<10 {
			continue
		}
		examples = append(examples, "Example outer arguments for "+name+" ("+kind+"): "+string(encoded)+".")
		if len(examples) >= 4 {
			break
		}
	}
	if len(examples) == 0 {
		return ""
	}
	return " " + strings.Join(examples, " ")
}

// exampleToolArguments fills only the small set of JSON Schema constructs
// needed to make a safe, non-executable relay example. It never copies a
// request or tool-call payload into the prompt; schema literals are bounded by
// the schema validation and the existing catalog prompt limits.
func exampleToolArguments(schema object, payloadField string, payload string) object {
	if schema == nil {
		return nil
	}
	result, ok := exampleSchemaValue(schema).(object)
	if !ok {
		return nil
	}
	result[payloadField] = payload
	return result
}

func exampleSchemaValue(schema object) any {
	if enum, ok := schema["enum"].([]any); ok && len(enum) > 0 {
		return enum[0]
	}
	if constant := schema["const"]; constant != nil {
		return constant
	}
	if choices, ok := schema["anyOf"].([]any); ok && len(choices) > 0 {
		if choice, ok := choices[0].(object); ok {
			return exampleSchemaValue(choice)
		}
	}
	if choices, ok := schema["oneOf"].([]any); ok && len(choices) > 0 {
		if choice, ok := choices[0].(object); ok {
			return exampleSchemaValue(choice)
		}
	}
	switch text(schema["type"]) {
	case "object":
		result := object{}
		properties, _ := schema["properties"].(object)
		required, _ := schema["required"].([]any)
		for _, raw := range required {
			name := text(raw)
			if property, ok := properties[name].(object); ok {
				result[name] = exampleSchemaValue(property)
			}
		}
		return result
	case "array":
		return []any{}
	case "boolean":
		return false
	case "integer", "number":
		return json.Number("1")
	case "string":
		return "example"
	default:
		return "example"
	}
}

func cloneObject(value object) object {
	copy := make(object, len(value))
	for key, item := range value {
		copy[key] = item
	}
	return copy
}

func mustJSON(value any) []byte {
	raw, _ := json.Marshal(value)
	return raw
}

func mustJSONText(value any) string {
	return string(mustJSON(value))
}

func quoted(value any) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

// Preserve constraints that are not expanded into prose, including schema references.
func describeSchema(value any, depth int) string {
	schema, ok := value.(object)
	if !ok || depth >= 8 {
		if value == nil {
			return "Use the arguments described by the tool."
		}
		return quoted(value)
	}
	var parts []string
	if kind := schema["type"]; kind != nil {
		parts = append(parts, "Value type: "+quoted(kind)+".")
	}
	if description := text(schema["description"]); description != "" {
		parts = append(parts, description)
	}
	required := make(map[string]bool)
	if names, ok := schema["required"].([]any); ok {
		for _, name := range names {
			required[text(name)] = true
		}
	}
	if properties, ok := schema["properties"].(object); ok {
		names := make([]string, 0, len(properties))
		for name := range properties {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			presence := "optional"
			if required[name] {
				presence = "required"
			}
			parts = append(parts, fmt.Sprintf("Field %s (%s): %s", quoted(name), presence, describeSchema(properties[name], depth+1)))
		}
	}
	if items := schema["items"]; items != nil {
		parts = append(parts, "Each array item: "+describeSchema(items, depth+1))
	}
	constraints := make(object)
	for key, value := range schema {
		switch key {
		case "type", "description", "properties", "items":
		default:
			constraints[key] = value
		}
	}
	if len(constraints) > 0 {
		parts = append(parts, "Additional constraints: "+quoted(constraints)+".")
	}
	if len(parts) == 0 {
		return "Any JSON value."
	}
	return strings.Join(parts, " ")
}
