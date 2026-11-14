package basispoints

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Compile once per request, preserve json.Number, and never resolve external
// schemas. Validation errors must not echo private argument values.
func compileToolSchema(parameters object) (*jsonschema.Schema, error) {
	if parameters == nil {
		return nil, nil
	}
	raw, err := json.Marshal(parameters)
	if err != nil || len(raw) > maxToolSchemaBytes {
		return nil, fmt.Errorf("basispoints tool schema exceeds %d MiB", maxToolSchemaBytes>>20)
	}
	if err := validateToolNumberBudget(parameters); err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	compiler.UseLoader(localSchemaLoader{})
	const schemaURL = "https://basispoints.invalid/client-tool.json"
	if err := compiler.AddResource(schemaURL, parameters); err != nil {
		return nil, toolSchemaCompileDiagnostic(err)
	}
	schema, err := compiler.Compile(schemaURL)
	if err != nil {
		return nil, toolSchemaCompileDiagnostic(err)
	}
	return schema, nil
}

// The compiler owns schema validity. Reuse its typed locations, never its error
// text or a second reference resolver that could disagree with its dialect rules.
func toolSchemaCompileDiagnostic(err error) *ContentValidationError {
	reason, location := "invalid_schema", ""
	var parts []string
	switch failure := err.(type) {
	case *jsonschema.LoadURLError:
		reason = "external_reference_blocked"
	case *jsonschema.JSONPointerNotFoundError, *jsonschema.AnchorNotFoundError:
		reason = "local_reference_not_found"
	case *jsonschema.SchemaValidationError:
		location = failure.URL
		if validation, ok := failure.Err.(*jsonschema.ValidationError); ok {
			for len(validation.Causes) > 0 {
				validation = validation.Causes[0]
			}
			parts = validation.InstanceLocation
		}
	case *jsonschema.InvalidRegexError:
		location = failure.URL
	}
	if parsed, parseErr := url.Parse(location); parseErr == nil && strings.HasPrefix(parsed.Fragment, "/") {
		prefix := strings.Split(parsed.Fragment[1:], "/")
		for i, part := range prefix {
			prefix[i] = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		}
		parts = append(prefix, parts...)
	}
	path := ""
	for _, part := range parts {
		if len(path) > 512 {
			path += "/[truncated]"
			break
		}
		part = toolDiagnosticIdentifier(part)
		path += "/" + strings.ReplaceAll(strings.ReplaceAll(part, "~", "~0"), "/", "~1")
	}
	if path != "" {
		path = "#" + path
	}
	return &ContentValidationError{Path: path, ContentType: "tool_schema", message: "basispoints tool schema rejected: " + reason}
}

func withToolSchemaContext(name, path string, err error) error {
	diagnostic := ContentValidationError{ContentType: "tool_schema", message: "basispoints tool schema exceeds validation resource limits"}
	var detail *ContentValidationError
	if errors.As(err, &detail) {
		diagnostic = *detail
	}
	if len(path) > 512 {
		path = path[:256] + "[truncated]" + path[len(path)-256:]
	}
	diagnostic.Path = path + diagnostic.Path
	diagnostic.message += " (tool=" + toolDiagnosticIdentifier(name) + ")"
	return &diagnostic
}

// The schema library expands JSON exponents into big.Rat. Bound expansion
// before compilation and validation so tiny exponent strings cannot allocate
// unbounded integers. Ordinary large IDs remain exact.
func validateToolNumberBudget(value any) error {
	budget := 65536
	var visit func(any, int) error
	visit = func(value any, depth int) error {
		if depth > 128 {
			return fmt.Errorf("basispoints tool schema or arguments exceed the nesting limit")
		}
		switch v := value.(type) {
		case json.Number:
			raw := v.String()
			if len(raw) > 4096 {
				return fmt.Errorf("basispoints tool number exceeds the precision limit")
			}
			digits := len(raw)
			if at := strings.IndexAny(raw, "eE"); at >= 0 {
				exponent, err := strconv.Atoi(raw[at+1:])
				if err != nil || exponent > 4096 || exponent < -4096 {
					return fmt.Errorf("basispoints tool number exceeds the exponent limit")
				}
				if exponent < 0 {
					exponent = -exponent
				}
				digits += exponent
			}
			budget -= digits
			if budget < 0 {
				return fmt.Errorf("basispoints tool numbers exceed the validation budget")
			}
		case object:
			for _, child := range v {
				if err := visit(child, depth+1); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range v {
				if err := visit(child, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return visit(value, 0)
}

type toolArgumentsSchemaError struct{}

func (toolArgumentsSchemaError) Error() string {
	return "basispoints function arguments do not satisfy the declared tool schema or validation resource limits"
}
