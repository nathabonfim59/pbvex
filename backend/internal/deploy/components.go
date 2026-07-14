package deploy

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	validators "github.com/nathabonfim59/pbvex/backend/internal/schema"
)

var (
	componentIdRe                 = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)
	componentRelativeModulePathRe = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_./\-]*\.ts$`)
)

// ComponentGraph is the canonical component DAG carried by a deployment manifest.
// The same component definition can be mounted multiple times; each mount is a
// node in the mount tree and has its own namespace.
type ComponentGraph struct {
	Definitions []ComponentDefinition `json:"definitions,omitempty"`
	Mounts      []ComponentMount      `json:"mounts,omitempty"`
}

// ComponentDefinition describes a reusable component package.
// It is independent of any mount name and is referenced by a deterministic hash.
type ComponentDefinition struct {
	ComponentID  string                      `json:"componentId"`
	ModulePaths  []string                    `json:"modulePaths"`
	ModuleHashes map[string]string           `json:"moduleHashes,omitempty"`
	Schema       JSONValue                   `json:"schema,omitempty"`
	Args         JSONValue                   `json:"args,omitempty"`
	Env          map[string]EnvArgDescriptor `json:"env,omitempty"`
	Dependencies []string                    `json:"dependencies,omitempty"`
}

// EnvArgDescriptor describes a component env value binding.
// Type is "value" for a literal string or "envVar" for a parent env reference.
type EnvArgDescriptor struct {
	Type  string `json:"type"`
	Value string `json:"value,omitempty"`
	Name  string `json:"name,omitempty"`
}

// ComponentMount is an instance of a component definition in the app tree.
// Name is the mount identity within the parent; full mount path is the path
// through the mount tree, e.g. "parent/child".
type ComponentMount struct {
	Name        string           `json:"name"`
	ComponentID string           `json:"componentId"`
	Args        JSONValue        `json:"args,omitempty"`
	Children    []ComponentMount `json:"children,omitempty"`
	// ArgsPresent tracks whether the "args" key was present in the JSON,
	// distinguishing explicit null from absent.
	ArgsPresent bool `json:"-"`
}

// MarshalJSON preserves omitted args versus explicit JSON null without
// exposing the internal ArgsPresent marker on the deployment protocol.
func (m ComponentMount) MarshalJSON() ([]byte, error) {
	if m.ArgsPresent || m.Args != nil {
		return json.Marshal(struct {
			Name        string           `json:"name"`
			ComponentID string           `json:"componentId"`
			Args        JSONValue        `json:"args"`
			Children    []ComponentMount `json:"children,omitempty"`
		}{m.Name, m.ComponentID, m.Args, m.Children})
	}
	return json.Marshal(struct {
		Name        string           `json:"name"`
		ComponentID string           `json:"componentId"`
		Children    []ComponentMount `json:"children,omitempty"`
	}{m.Name, m.ComponentID, m.Children})
}

// UnmarshalJSON retains presence even when args is null.
func (m *ComponentMount) UnmarshalJSON(data []byte) error {
	var raw struct {
		Name        string           `json:"name"`
		ComponentID string           `json:"componentId"`
		Args        json.RawMessage  `json:"args"`
		Children    []ComponentMount `json:"children"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.Name, m.ComponentID, m.Children = raw.Name, raw.ComponentID, raw.Children
	m.ArgsPresent = raw.Args != nil
	m.Args = nil
	if raw.Args != nil {
		if err := json.Unmarshal(raw.Args, &m.Args); err != nil {
			return err
		}
	}
	return nil
}

// MountPath returns the path of this mount from its parent path.
// The root mount path is empty.
func (m ComponentMount) MountPath(parent string) string {
	if parent == "" {
		return m.Name
	}
	if m.Name == "" {
		return parent
	}
	return parent + "/" + m.Name
}

// ValidateComponents validates the component graph attached to a manifest.
// It checks identifiers, duplicate mounts, cycles, missing definitions,
// module path collisions, and component arg values against their definition.
func ValidateComponents(value any) (*ComponentGraph, error) {
	if value == nil {
		return nil, nil
	}
	o, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("components must be an object")
	}
	if !onlyKeys(o, "definitions", "mounts") {
		return nil, fmt.Errorf("components has unknown fields")
	}
	defs, err := validateComponentDefinitions(o["definitions"])
	if err != nil {
		return nil, fmt.Errorf("components.definitions: %w", err)
	}
	if len(defs) > 1024 {
		return nil, fmt.Errorf("components.definitions exceeds 1024")
	}
	if err := checkComponentDependencies(defs); err != nil {
		return nil, fmt.Errorf("components.dependencies: %w", err)
	}
	mounts, err := validateComponentMounts(o["mounts"], defs)
	if err != nil {
		return nil, fmt.Errorf("components.mounts: %w", err)
	}
	if err := validateComponentMountModuleOwnership(defs, mounts); err != nil {
		return nil, fmt.Errorf("components.mounts: %w", err)
	}
	if len(defs) == 0 && len(mounts) == 0 {
		return nil, nil
	}
	return &ComponentGraph{
		Definitions: defs,
		Mounts:      mounts,
	}, nil
}

func validateComponentMountModuleOwnership(definitions []ComponentDefinition, mounts []ComponentMount) error {
	definitionsByID := make(map[string]ComponentDefinition, len(definitions))
	for _, definition := range definitions {
		definitionsByID[definition.ComponentID] = definition
	}
	var visit func(ComponentMount, string) error
	visit = func(mount ComponentMount, parent string) error {
		path := mount.MountPath(parent)
		childNames := make(map[string]struct{}, len(mount.Children))
		for _, child := range mount.Children {
			childNames[child.Name] = struct{}{}
		}
		definition := definitionsByID[mount.ComponentID]
		for _, relative := range definition.ModulePaths {
			firstSegment := relative
			if slash := strings.IndexByte(relative, '/'); slash >= 0 {
				firstSegment = relative[:slash]
			}
			if _, exists := childNames[firstSegment]; exists {
				return fmt.Errorf("component module %q from mount %q collides with descendant mount %q", path+"/"+relative, path, path+"/"+firstSegment)
			}
		}
		for _, child := range mount.Children {
			if err := visit(child, path); err != nil {
				return err
			}
		}
		return nil
	}
	for _, mount := range mounts {
		if err := visit(mount, ""); err != nil {
			return err
		}
	}
	return nil
}

func validateComponentDefinitions(value any) ([]ComponentDefinition, error) {
	arr, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("definitions must be an array")
	}
	out := make([]ComponentDefinition, 0, len(arr))
	seen := map[string]bool{}
	for i, raw := range arr {
		o, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("definitions[%d] must be an object", i)
		}
		if !onlyKeys(o, "componentId", "modulePaths", "moduleHashes", "schema", "args", "env", "dependencies") {
			return nil, fmt.Errorf("definitions[%d] has unknown fields", i)
		}
		id, ok := o["componentId"].(string)
		if !ok || !isComponentId(id) {
			return nil, fmt.Errorf("definitions[%d] componentId is invalid", i)
		}
		if seen[id] {
			return nil, fmt.Errorf("definitions[%d] duplicate componentId %q", i, id)
		}
		seen[id] = true

		modulePaths, err := validateComponentModulePaths(o["modulePaths"])
		if err != nil {
			return nil, fmt.Errorf("definitions[%d] modulePaths: %w", i, err)
		}
		moduleHashes, err := validateComponentModuleHashes(o["moduleHashes"], modulePaths)
		if err != nil {
			return nil, fmt.Errorf("definitions[%d] moduleHashes: %w", i, err)
		}
		var schema JSONValue
		if v, present := o["schema"]; present && v != nil {
			schema, err = validateSchema(v)
			if err != nil {
				return nil, fmt.Errorf("definitions[%d] schema: %w", i, err)
			}
		}
		var args JSONValue
		if v, present := o["args"]; present && v != nil {
			if !validators.ValidateDescriptor(v) {
				return nil, fmt.Errorf("definitions[%d] args is not a valid validator descriptor", i)
			}
			args = v
		}
		env, err := validateComponentEnv(o["env"])
		if err != nil {
			return nil, fmt.Errorf("definitions[%d] env: %w", i, err)
		}
		dependencies, err := validateComponentDependencies(o["dependencies"], seen)
		if err != nil {
			return nil, fmt.Errorf("definitions[%d] dependencies: %w", i, err)
		}
		def := ComponentDefinition{
			ComponentID:  id,
			ModulePaths:  modulePaths,
			ModuleHashes: moduleHashes,
			Schema:       schema,
			Args:         args,
			Env:          env,
			Dependencies: dependencies,
		}
		out = append(out, def)
	}
	return out, nil
}

func validateComponentModulePaths(value any) ([]string, error) {
	if value == nil {
		return nil, fmt.Errorf("modulePaths must be an array")
	}
	arr, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("modulePaths must be an array")
	}
	if len(arr) == 0 {
		return nil, fmt.Errorf("modulePaths must be a non-empty array")
	}
	out := make([]string, 0, len(arr))
	seen := map[string]bool{}
	for i, raw := range arr {
		s, ok := raw.(string)
		if !ok || !isComponentRelativeModulePath(s) {
			return nil, fmt.Errorf("modulePaths[%d] is invalid: %q", i, raw)
		}
		if seen[s] {
			return nil, fmt.Errorf("modulePaths[%d] duplicate %q", i, s)
		}
		seen[s] = true
		out = append(out, s)
	}
	return out, nil
}

func isComponentRelativeModulePath(s string) bool {
	if s == "" || len(s) > MaxPathLength {
		return false
	}
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, "\\") || strings.Contains(s, "..") || strings.Contains(s, ":") {
		return false
	}
	return componentRelativeModulePathRe.MatchString(s)
}

func validateComponentModuleHashes(value any, modulePaths []string) (map[string]string, error) {
	if value == nil {
		return nil, fmt.Errorf("moduleHashes must be an object")
	}
	o, ok := value.(map[string]any)
	if !ok || len(o) == 0 {
		return nil, fmt.Errorf("moduleHashes must be an object")
	}
	out := make(map[string]string, len(modulePaths))
	for _, p := range modulePaths {
		h, ok := o[p].(string)
		if !ok || !IsSha256Hex(h) {
			return nil, fmt.Errorf("moduleHashes[%q] is not a SHA-256 hex", p)
		}
		out[p] = h
	}
	for k := range o {
		if !contains(modulePaths, k) {
			return nil, fmt.Errorf("moduleHashes contains key not in modulePaths: %q", k)
		}
	}
	return out, nil
}

func validateComponentDependencies(value any, seen map[string]bool) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	arr, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("dependencies must be an array")
	}
	out := make([]string, 0, len(arr))
	seenDeps := map[string]bool{}
	for i, raw := range arr {
		s, ok := raw.(string)
		if !ok || !isComponentId(s) {
			return nil, fmt.Errorf("dependencies[%d] is invalid", i)
		}
		if seenDeps[s] {
			return nil, fmt.Errorf("dependencies[%d] duplicate %q", i, s)
		}
		seenDeps[s] = true
		out = append(out, s)
	}
	return out, nil
}

func checkComponentDependencies(defs []ComponentDefinition) error {
	defMap := map[string]ComponentDefinition{}
	for _, d := range defs {
		defMap[d.ComponentID] = d
	}
	for _, d := range defs {
		if err := checkComponentDependency(&d, defMap, map[string]bool{}, 0); err != nil {
			return err
		}
	}
	return nil
}

func checkComponentDependency(def *ComponentDefinition, defs map[string]ComponentDefinition, stack map[string]bool, depth int) error {
	if depth > 32 {
		return fmt.Errorf("component dependency depth exceeds 32")
	}
	if stack[def.ComponentID] {
		return fmt.Errorf("cyclic component dependency")
	}
	stack[def.ComponentID] = true
	for _, dep := range def.Dependencies {
		depDef, ok := defs[dep]
		if !ok {
			return fmt.Errorf("component dependency %q is not defined", dep)
		}
		if err := checkComponentDependency(&depDef, defs, stack, depth+1); err != nil {
			return err
		}
	}
	delete(stack, def.ComponentID)
	return nil
}

func validateComponentEnv(value any) (map[string]EnvArgDescriptor, error) {
	if value == nil {
		return nil, nil
	}
	o, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("env must be an object")
	}
	out := make(map[string]EnvArgDescriptor, len(o))
	for k, raw := range o {
		if !isSafeFieldName(k) {
			return nil, fmt.Errorf("env key %q is invalid", k)
		}
		desc, err := validateEnvArgDescriptor(raw)
		if err != nil {
			return nil, fmt.Errorf("env %q: %w", k, err)
		}
		out[k] = desc
	}
	return out, nil
}

func validateEnvArgDescriptor(value any) (EnvArgDescriptor, error) {
	o, ok := value.(map[string]any)
	if !ok {
		return EnvArgDescriptor{}, fmt.Errorf("env arg must be an object")
	}
	if !onlyKeys(o, "type", "value", "name") {
		return EnvArgDescriptor{}, fmt.Errorf("env arg has unknown fields")
	}
	t, ok := o["type"].(string)
	if !ok || (t != "value" && t != "envVar") {
		return EnvArgDescriptor{}, fmt.Errorf("env arg type must be 'value' or 'envVar'")
	}
	desc := EnvArgDescriptor{Type: t}
	if v, ok := o["value"].(string); ok {
		desc.Value = v
	}
	if n, ok := o["name"].(string); ok {
		desc.Name = n
	}
	if t == "value" && desc.Value == "" {
		return EnvArgDescriptor{}, fmt.Errorf("env arg value must be a string")
	}
	if t == "envVar" && desc.Name == "" {
		return EnvArgDescriptor{}, fmt.Errorf("env arg name must be a string")
	}
	return desc, nil
}

func validateComponentMounts(value any, defs []ComponentDefinition) ([]ComponentMount, error) {
	arr, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("mounts must be an array")
	}
	defMap := map[string]*ComponentDefinition{}
	for i := range defs {
		defMap[defs[i].ComponentID] = &defs[i]
	}
	out := make([]ComponentMount, 0, len(arr))
	seen := map[string]bool{}
	for i, raw := range arr {
		m, err := validateComponentMount(raw, "", defMap, seen, 1)
		if err != nil {
			return nil, fmt.Errorf("mounts[%d]: %w", i, err)
		}
		out = append(out, *m)
	}
	// Duplicate paths are already rejected by the per-path seen check inside
	// validateComponentMount; do not compare out length (top-level only) to seen
	// length (includes nested children), which would false-positive on nesting.
	return out, nil
}

func validateComponentMount(raw any, parentPath string, defs map[string]*ComponentDefinition, seen map[string]bool, depth int) (*ComponentMount, error) {
	if depth > 32 {
		return nil, fmt.Errorf("mount depth exceeds 32")
	}
	o, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("mount must be an object")
	}
	if !onlyKeys(o, "name", "componentId", "args", "children") {
		return nil, fmt.Errorf("mount has unknown fields")
	}
	name, ok := o["name"].(string)
	if !ok || !isIdentifier(name) {
		return nil, fmt.Errorf("mount name is invalid")
	}
	path := name
	if parentPath != "" {
		path = parentPath + "/" + name
	}
	if seen[path] {
		return nil, fmt.Errorf("duplicate mount path %q", path)
	}
	seen[path] = true

	componentID, ok := o["componentId"].(string)
	if !ok || !isComponentId(componentID) {
		return nil, fmt.Errorf("mount componentId is invalid")
	}
	def, ok := defs[componentID]
	if !ok {
		return nil, fmt.Errorf("mount %q references unknown component %q", path, componentID)
	}
	var args JSONValue
	argsPresent := false
	if v, present := o["args"]; present {
		argsPresent = true
		if def.Args == nil {
			return nil, fmt.Errorf("mount %q does not accept args", path)
		}
		if v != nil && !isJsonValue(v, 0, map[uintptr]struct{}{}) {
			return nil, fmt.Errorf("mount args must be a valid JSON value")
		}
		args = v
		if !validators.ValidateComponentValue(def.Args, args) {
			return nil, fmt.Errorf("mount %q args validation failed", path)
		}
	} else if def.Args != nil && !isOptionalDescriptor(def.Args) {
		return nil, fmt.Errorf("mount %q requires args", path)
	}

	children := []ComponentMount{}
	if rawChildren, present := o["children"]; present && rawChildren != nil {
		arr, ok := rawChildren.([]any)
		if !ok {
			return nil, fmt.Errorf("mount children must be an array")
		}
		for i, rawChild := range arr {
			child, err := validateComponentMount(rawChild, path, defs, seen, depth+1)
			if err != nil {
				return nil, fmt.Errorf("children[%d]: %w", i, err)
			}
			children = append(children, *child)
		}
	}

	return &ComponentMount{
		Name:        name,
		ComponentID: componentID,
		Args:        args,
		ArgsPresent: argsPresent,
		Children:    children,
	}, nil
}

func isComponentId(s string) bool {
	return s != "" && len(s) <= MaxIdentifierLength && componentIdRe.MatchString(s)
}

// VerifyModuleSources recomputes the canonical SHA-256 of each uploaded module
// from its actual bytes and rejects missing/extra/mismatched module paths for
// the component graph. This ties the manifest's declared moduleHashes (and
// therefore the content-addressed componentId) to the actual uploaded
// executable module bytes, not client-declared hashes.
func VerifyModuleSources(modules []ModuleSource, manifest DeploymentManifest) error {
	if manifest.Components == nil {
		return nil
	}
	hashByPath := make(map[string]string, len(modules))
	for _, m := range modules {
		raw, err := base64.StdEncoding.DecodeString(m.Bytes)
		if err != nil {
			return fmt.Errorf("module %q bytes are not valid base64", m.Path)
		}
		h := sha256.Sum256(raw)
		hashByPath[m.Path] = hex.EncodeToString(h[:])
	}
	defsByID := map[string]ComponentDefinition{}
	for _, d := range manifest.Components.Definitions {
		defsByID[d.ComponentID] = d
	}
	// Collect every declared module full path (mount x modulePath) and verify
	// each is present with a matching hash. Use a set so the global "extra"
	// check does not false-positive on nested mounts (a child module's full
	// path is also a string-prefix of the parent's prefix).
	declared := map[string]struct{}{}
	var walk func(m ComponentMount, parent string) error
	walk = func(m ComponentMount, parent string) error {
		path := m.MountPath(parent)
		if def, ok := defsByID[m.ComponentID]; ok {
			for _, rel := range def.ModulePaths {
				full := componentMountPrefix + path + "/" + rel
				declared[full] = struct{}{}
				got, ok := hashByPath[full]
				if !ok {
					return fmt.Errorf("module %q is missing from the upload", full)
				}
				want, ok := def.ModuleHashes[rel]
				if !ok || got != want {
					return fmt.Errorf("module %q hash mismatch", full)
				}
			}
		}
		for _, child := range m.Children {
			if err := walk(child, m.MountPath(parent)); err != nil {
				return err
			}
		}
		return nil
	}
	for _, m := range manifest.Components.Mounts {
		if err := walk(m, ""); err != nil {
			return err
		}
	}
	// Global extra check: every uploaded module under the component namespace
	// must be declared by some mount.
	for fullPath := range hashByPath {
		if strings.HasPrefix(fullPath, componentMountPrefix) {
			if _, ok := declared[fullPath]; !ok {
				return fmt.Errorf("module %q is not declared by any component mount", fullPath)
			}
		}
	}
	return nil
}

// componentMountPrefix is the canonical namespace prefix for component module
// paths, mirroring the runtime bridge.
const componentMountPrefix = "pbvex/components/"

// ComponentMountForModule returns the deepest mount owning modulePath. The
// manifest validator guarantees that the relative module belongs to the
// returned definition, so runtime and schema code can use this as the single
// namespace-resolution primitive.
func ComponentMountForModule(graph *ComponentGraph, modulePath string) (ComponentMount, bool) {
	if graph == nil || !strings.HasPrefix(modulePath, componentMountPrefix) {
		return ComponentMount{}, false
	}
	var best ComponentMount
	bestPath := ""
	var walk func([]ComponentMount, string)
	walk = func(mounts []ComponentMount, parent string) {
		for _, mount := range mounts {
			path := mount.MountPath(parent)
			prefix := componentMountPrefix + path + "/"
			if strings.HasPrefix(modulePath, prefix) && len(path) > len(bestPath) {
				best, bestPath = mount, path
			}
			walk(mount.Children, path)
		}
	}
	walk(graph.Mounts, "")
	return best, bestPath != ""
}

// ComponentMountPathForModule is the path-bearing companion used to derive a
// stable namespace. It deliberately hashes the canonical mount path, not the
// content-addressed component definition, so upgrades preserve data.
func ComponentMountPathForModule(graph *ComponentGraph, modulePath string) (string, ComponentMount, bool) {
	if graph == nil || !strings.HasPrefix(modulePath, componentMountPrefix) {
		return "", ComponentMount{}, false
	}
	var best ComponentMount
	bestPath := ""
	var walk func([]ComponentMount, string)
	walk = func(mounts []ComponentMount, parent string) {
		for _, mount := range mounts {
			path := mount.MountPath(parent)
			if strings.HasPrefix(modulePath, componentMountPrefix+path+"/") && len(path) > len(bestPath) {
				best, bestPath = mount, path
			}
			walk(mount.Children, path)
		}
	}
	walk(graph.Mounts, "")
	return bestPath, best, bestPath != ""
}

// validateComponentFunctionBinding ensures that every function whose modulePath
// falls under a component mount namespace belongs to a module declared by that
// mount's component definition. This binds declared module paths to the
// functions that may run inside a mount, preventing a manifest from injecting
// functions into a component namespace the component did not declare.
func validateComponentFunctionBinding(functions []FunctionDescriptor, graph *ComponentGraph) error {
	if graph == nil {
		return nil
	}
	type mountEntry struct {
		prefix      string
		modulePaths map[string]struct{}
		path        string
	}
	defsByID := map[string]ComponentDefinition{}
	for _, d := range graph.Definitions {
		defsByID[d.ComponentID] = d
	}
	var mounts []mountEntry
	collect := func(m ComponentMount, parent string) {
		path := m.MountPath(parent)
		prefix := componentMountPrefix + path + "/"
		def, ok := defsByID[m.ComponentID]
		if !ok {
			return
		}
		mod := map[string]struct{}{}
		for _, p := range def.ModulePaths {
			mod[p] = struct{}{}
		}
		mounts = append(mounts, mountEntry{prefix: prefix, modulePaths: mod, path: path})
	}
	var walk func(m ComponentMount, parent string)
	walk = func(m ComponentMount, parent string) {
		collect(m, parent)
		for _, child := range m.Children {
			walk(child, m.MountPath(parent))
		}
	}
	for _, m := range graph.Mounts {
		walk(m, "")
	}
	// Longest-prefix-first, mirroring Bridge.mountForModulePath.
	sort.Slice(mounts, func(i, j int) bool {
		return len(mounts[i].prefix) > len(mounts[j].prefix)
	})
	for _, fn := range functions {
		if !strings.HasPrefix(fn.ModulePath, componentMountPrefix) {
			continue
		}
		matched := false
		for _, mt := range mounts {
			if strings.HasPrefix(fn.ModulePath, mt.prefix) {
				relative := strings.TrimPrefix(fn.ModulePath, mt.prefix)
				if _, ok := mt.modulePaths[relative]; !ok {
					return fmt.Errorf("function %q modulePath %q is not declared by mount %q", fn.Name, fn.ModulePath, mt.path)
				}
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("function %q modulePath %q does not match any component mount", fn.Name, fn.ModulePath)
		}
	}
	return nil
}

func isOptionalDescriptor(value any) bool {
	o, ok := value.(map[string]any)
	if !ok {
		return false
	}
	t := o["type"]
	if t == "optional" || t == "defaulted" {
		return true
	}
	if t == "union" {
		validators, _ := o["validators"].([]any)
		for _, v := range validators {
			if isOptionalDescriptor(v) {
				return true
			}
		}
	}
	if t == "object" {
		shape, ok := o["shape"].(map[string]any)
		if !ok {
			return false
		}
		for _, field := range shape {
			if !isOptionalDescriptor(field) {
				return false
			}
		}
		return true
	}
	return false
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// componentIDHashPrefix marks content-addressed componentIds produced by the
// canonical bundler. Component identifiers MUST be content-addressed (def_ +
// canonical hash of the definition) so that a manifest cannot bypass integrity
// with an opaque or hand-picked id.
const componentIDHashPrefix = "def_"

// ComputeComponentID returns the canonical content-addressed componentId for a
// component definition. bundleSha is the verified SHA-256 hex of the executable
// bundle; including it in the hash input binds the componentId to the exact
// bytes the runtime will execute, not just the declared module sources.
// It mirrors the TS bundler's buildComponentGraph hash so Go-generated and
// TS-generated ids are byte-identical.
func ComputeComponentID(def ComponentDefinition, bundleSha string) string {
	hash, err := CanonicalHash(componentHashInput(def, bundleSha))
	if err != nil {
		return ""
	}
	return componentIDHashPrefix + hash
}

// AuthenticateComponentIDs verifies every declared componentId is the canonical
// content-addressed hash of its definition, including the verified bundleSha.
// This binds componentId to the exact executable bundle bytes. It mirrors
// deploy.authenticateComponentID and the TS authenticateComponentIds.
func AuthenticateComponentIDs(manifest DeploymentManifest, bundleSha string) error {
	if manifest.Components == nil {
		return nil
	}
	for _, def := range manifest.Components.Definitions {
		if err := authenticateComponentID(def, bundleSha); err != nil {
			return fmt.Errorf("components.definitions componentId %q: %w", def.ComponentID, err)
		}
	}
	return nil
}

// authenticateComponentID verifies that a componentId is content-addressed and
// matches the canonical hash of its declared definition, including bundleSha.
// It mirrors the hash computed by the TS bundler (buildComponentGraph) so that
// a manifest cannot lie about a component's content. Non content-addressed ids
// are rejected so integrity cannot be bypassed.
func authenticateComponentID(def ComponentDefinition, bundleSha string) error {
	if !hasComponentIDHashPrefix(def.ComponentID) {
		return fmt.Errorf("must be content-addressed (%q prefix)", componentIDHashPrefix)
	}
	hashInput := componentHashInput(def, bundleSha)
	expected, err := CanonicalHash(hashInput)
	if err != nil {
		return fmt.Errorf("could not compute component hash: %w", err)
	}
	if def.ComponentID != componentIDHashPrefix+expected {
		return fmt.Errorf("does not match content hash")
	}
	return nil
}

func hasComponentIDHashPrefix(id string) bool {
	return len(id) > len(componentIDHashPrefix) && id[:len(componentIDHashPrefix)] == componentIDHashPrefix
}

// componentHashInput reconstructs the canonical hash input for a component
// definition. bundleSha (the verified SHA-256 hex of the executable bundle) is
// always included so the componentId binds to the exact bytes the runtime
// executes. The shape and key set must match the TS bundler exactly so the
// resulting canonical JSON is byte-identical.
func componentHashInput(def ComponentDefinition, bundleSha string) map[string]any {
	modulePaths := make([]any, len(def.ModulePaths))
	for i, p := range def.ModulePaths {
		modulePaths[i] = p
	}
	moduleHashes := make(map[string]any, len(def.ModuleHashes))
	for k, v := range def.ModuleHashes {
		moduleHashes[k] = v
	}
	dependencies := make([]any, len(def.Dependencies))
	for i, d := range def.Dependencies {
		dependencies[i] = d
	}
	out := map[string]any{
		"modulePaths":  modulePaths,
		"moduleHashes": moduleHashes,
		"dependencies": dependencies,
		"bundleSha":    bundleSha,
	}
	if def.Schema != nil {
		out["schema"] = def.Schema
	}
	if def.Args != nil {
		out["args"] = def.Args
	}
	if def.Env != nil {
		env := make(map[string]any, len(def.Env))
		for name, desc := range def.Env {
			entry := map[string]any{"type": desc.Type}
			if desc.Value != "" {
				entry["value"] = desc.Value
			}
			if desc.Name != "" {
				entry["name"] = desc.Name
			}
			env[name] = entry
		}
		out["env"] = env
	}
	return out
}
