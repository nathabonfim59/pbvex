package deploy

import (
	"crypto/sha256"
	"encoding/base32"
	"fmt"
	"strings"
)

const RootNamespace = "root"

var componentBase32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// ComponentNamespaceID is stable for a canonical mount path across component
// upgrades and deployment IDs. Renaming a mount intentionally creates a new
// namespace and leaves the old data dormant.
func ComponentNamespaceID(path string) (string, error) {
	if path == "" || strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") || strings.Contains(path, "//") {
		return "", fmt.Errorf("invalid component mount path")
	}
	for _, segment := range strings.Split(path, "/") {
		if !isIdentifier(segment) {
			return "", fmt.Errorf("invalid component mount path")
		}
	}
	sum := sha256.Sum256([]byte("pbvex:component-namespace:v1\x00" + path))
	return "cmp_" + strings.ToLower(componentBase32.EncodeToString(sum[:])), nil
}

// ComponentCollectionName maps one logical component table to one durable
// PocketBase collection. It is deliberately independent of deployment and
// component content hashes, and bounded independently of user table length.
func ComponentCollectionName(namespace, table string) (string, error) {
	if !strings.HasPrefix(namespace, "cmp_") || !isIdentifier(table) {
		return "", fmt.Errorf("invalid component collection identity")
	}
	sum := sha256.Sum256([]byte("pbvex:component-table:v1\x00" + namespace + "\x00" + table))
	return "pbvex_cmp_" + strings.ToLower(componentBase32.EncodeToString(sum[:])), nil
}

type ComponentNamespace struct {
	ID              string
	Path            string
	Mount           ComponentMount
	Definition      ComponentDefinition
	Schema          JSONValue
	PhysicalByTable map[string]string
}

// ComponentNamespaces builds the validated deployment catalog. The returned
// map is keyed by mount path and therefore permits the same definition to be
// mounted repeatedly without sharing storage.
func ComponentNamespaces(graph *ComponentGraph) (map[string]ComponentNamespace, error) {
	out := map[string]ComponentNamespace{}
	if graph == nil {
		return out, nil
	}
	defs := make(map[string]ComponentDefinition, len(graph.Definitions))
	for _, definition := range graph.Definitions {
		defs[definition.ComponentID] = definition
	}
	var walk func([]ComponentMount, string) error
	walk = func(mounts []ComponentMount, parent string) error {
		for _, mount := range mounts {
			path := mount.MountPath(parent)
			definition, ok := defs[mount.ComponentID]
			if !ok {
				return fmt.Errorf("component definition unavailable")
			}
			id, err := ComponentNamespaceID(path)
			if err != nil {
				return err
			}
			physical := map[string]string{}
			if schema, ok := definition.Schema.(map[string]any); ok {
				for _, raw := range listJSON(schema["tables"]) {
					table, _ := raw.(map[string]any)["tableName"].(string)
					name, err := ComponentCollectionName(id, table)
					if err != nil {
						return err
					}
					physical[table] = name
				}
			}
			out[path] = ComponentNamespace{ID: id, Path: path, Mount: mount, Definition: definition, Schema: definition.Schema, PhysicalByTable: physical}
			if err := walk(mount.Children, path); err != nil {
				return err
			}
		}
		return nil
	}
	return out, walk(graph.Mounts, "")
}

func listJSON(value any) []any {
	items, _ := value.([]any)
	return items
}

// NamespaceForModule resolves a function to its mount catalog entry.
func NamespaceForModule(manifest DeploymentManifest, modulePath string) (ComponentNamespace, bool) {
	path, _, ok := ComponentMountPathForModule(manifest.Components, modulePath)
	if !ok {
		return ComponentNamespace{}, false
	}
	namespaces, err := ComponentNamespaces(manifest.Components)
	if err != nil {
		return ComponentNamespace{}, false
	}
	ns, ok := namespaces[path]
	return ns, ok
}
