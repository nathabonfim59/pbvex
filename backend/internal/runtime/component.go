package runtime

import (
	"fmt"
	"os"
	"sort"

	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/nathabonfim59/pbvex/backend/internal/schema"
)

var missingComponentValue = &struct{}{}

func resolveComponentArgs(namespace deploy.ComponentNamespace, check schema.IDChecker) (any, bool, error) {
	descriptor := namespace.Definition.Args
	if descriptor == nil {
		return map[string]any{}, true, nil
	}
	if namespace.Mount.ArgsPresent {
		normalized, err := schema.NormalizeValue(descriptor, namespace.Mount.Args, check)
		if err != nil {
			return nil, false, fmt.Errorf("component %q mount args are invalid", namespace.Path)
		}
		return normalized, true, nil
	}
	value, accepted := resolveMissingComponentArg(descriptor)
	if !accepted || value == missingComponentValue {
		return nil, false, nil
	}
	normalized, err := schema.NormalizeValue(descriptor, value, check)
	if err != nil {
		return nil, false, fmt.Errorf("component %q mount args are invalid", namespace.Path)
	}
	return normalized, true, nil
}

func resolveMissingComponentArg(descriptor any) (any, bool) {
	raw, ok := descriptor.(map[string]any)
	if !ok {
		return nil, false
	}
	switch raw["type"] {
	case "optional":
		return missingComponentValue, true
	case "defaulted":
		value, ok := raw["defaultValue"]
		if !ok {
			return nil, false
		}
		return resolveComponentDefaults(raw, value), true
	case "union":
		branches, _ := raw["validators"].([]any)
		for _, branch := range branches {
			if value, ok := resolveMissingComponentArg(branch); ok {
				return value, true
			}
		}
	case "object":
		shape, _ := raw["shape"].(map[string]any)
		if shape == nil {
			shape, _ = raw["fields"].(map[string]any)
		}
		if shape == nil {
			return nil, false
		}
		for _, child := range shape {
			if _, ok := resolveMissingComponentArg(child); !ok {
				return nil, false
			}
		}
		return resolveComponentDefaults(raw, map[string]any{}), true
	}
	return nil, false
}

func resolveComponentDefaults(descriptor, value any) any {
	raw, ok := descriptor.(map[string]any)
	if !ok {
		return value
	}
	switch raw["type"] {
	case "defaulted", "optional":
		return resolveComponentDefaults(raw["validator"], value)
	case "object":
		shape, _ := raw["shape"].(map[string]any)
		if shape == nil {
			shape, _ = raw["fields"].(map[string]any)
		}
		object, ok := value.(map[string]any)
		if !ok {
			return value
		}
		out := make(map[string]any, len(object))
		for key, item := range object {
			out[key] = item
		}
		for key, child := range shape {
			if item, present := out[key]; present {
				out[key] = resolveComponentDefaults(child, item)
			} else if resolved, accepted := resolveMissingComponentArg(child); accepted && resolved != missingComponentValue {
				out[key] = resolved
			}
		}
		return out
	case "array":
		items, ok := value.([]any)
		if !ok {
			return value
		}
		out := make([]any, len(items))
		for i, item := range items {
			out[i] = resolveComponentDefaults(raw["item"], item)
		}
		return out
	case "record":
		object, ok := value.(map[string]any)
		if !ok {
			return value
		}
		out := make(map[string]any, len(object))
		for key, item := range object {
			out[key] = resolveComponentDefaults(raw["value"], item)
		}
		return out
	case "union":
		branches, _ := raw["validators"].([]any)
		for _, branch := range branches {
			if schema.ValidateValue(branch, value, nil) {
				return resolveComponentDefaults(branch, value)
			}
		}
	}
	return value
}

func resolveComponentEnv(namespace deploy.ComponentNamespace) (map[string]string, error) {
	names := make([]string, 0, len(namespace.Definition.Env))
	for name := range namespace.Definition.Env {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make(map[string]string, len(names))
	for _, name := range names {
		binding := namespace.Definition.Env[name]
		switch binding.Type {
		case "value":
			out[name] = binding.Value
		case "envVar":
			value, ok := os.LookupEnv(binding.Name)
			if !ok {
				return nil, fmt.Errorf("component %q env %q requires unset variable %q", namespace.Path, name, binding.Name)
			}
			out[name] = value
		default:
			return nil, fmt.Errorf("component %q env %q is invalid", namespace.Path, name)
		}
	}
	return out, nil
}
