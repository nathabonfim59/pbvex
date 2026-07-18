package pbvex

import (
	"context"
	"fmt"

	"github.com/dop251/goja"
	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/nathabonfim59/pbvex/backend/internal/runtime"
	"github.com/nathabonfim59/pbvex/backend/internal/storage"
	"github.com/pocketbase/pocketbase/core"
)

// storageExtender attaches only ctx.storage. Base database, scheduler, and
// future auth capabilities are owned by their respective runtime builders and
// must never be replaced here. Mutations, actions, and HTTP actions receive the
// full storage surface; queries receive only getUrl.
func storageExtender(storageService *storage.Service) runtime.ContextExtender {
	return func(vm *goja.Runtime, ctx context.Context, app core.App, fd deploy.FunctionDescriptor, obj *goja.Object) error {
		auth, _ := runtime.AuthFromContext(ctx)

		// Participate in the caller's transaction when present so storage writes
		// do not contend with the outer mutation transaction for the DB lock.
		storageCtx := ctx
		if app != nil {
			storageCtx = storage.WithApp(ctx, app)
		}

		isWriter := fd.Type == deploy.FunctionTypeMutation || fd.Type == deploy.FunctionTypeAction || fd.Type == deploy.FunctionTypeHTTPAction
		isReader := isWriter || fd.Type == deploy.FunctionTypeQuery
		if !isReader {
			return nil
		}

		storageObj := vm.NewObject()
		if isReader {
			storageObj.Set("getUrl", vm.ToValue(func(call goja.FunctionCall) goja.Value {
				return wrapPromise(vm, func() (any, error) {
					id := extractString(call, 0)
					if id == "" {
						return goja.Null(), nil
					}
					mode, err := extractStorageURLMode(vm, call)
					if err != nil {
						return nil, err
					}
					var url string
					if mode == "capability" {
						url, err = storageService.GetCapabilityURL(storageCtx, id)
					} else if mode == "public" {
						url, err = storageService.GetPublicURL(storageCtx, id)
					} else {
						url, err = storageService.GetURL(storageCtx, id, storage.AuthContext{
							IsAuthenticated: auth.IsAuthenticated,
							TokenIdentifier: auth.TokenIdentifier,
						})
					}
					if err != nil {
						return nil, err
					}
					if url == "" {
						return goja.Null(), nil
					}
					return url, nil
				})
			}))
			storageObj.Set("getMetadata", vm.ToValue(func(call goja.FunctionCall) goja.Value {
				return wrapPromise(vm, func() (any, error) {
					metadata, err := storageService.GetMetadata(storageCtx, extractString(call, 0))
					if err != nil {
						return nil, err
					}
					if metadata == nil {
						return goja.Null(), nil
					}
					return metadata, nil
				})
			}))
		}
		if isWriter {
			storageObj.Set("generateUploadUrl", vm.ToValue(func(call goja.FunctionCall) goja.Value {
				return wrapPromise(vm, func() (any, error) {
					authContext := storage.AuthContext{
						IsAuthenticated: auth.IsAuthenticated,
						TokenIdentifier: auth.TokenIdentifier,
					}
					options, err := extractImageUploadOptions(call)
					if err != nil {
						return nil, err
					}
					if options == nil {
						return storageService.GenerateUploadURL(storageCtx, authContext)
					}
					manifest, ok := runtime.ManifestFromContext(ctx)
					if !ok {
						return nil, fmt.Errorf("deployment manifest unavailable")
					}
					policy, err := imagePolicyForField(manifest, fd.ModulePath, options["table"], options["field"])
					if err != nil {
						return nil, err
					}
					url, err := storageService.GenerateImageUploadURL(storageCtx, authContext, policy)
					return url, err
				})
			}))
			storageObj.Set("delete", vm.ToValue(func(call goja.FunctionCall) goja.Value {
				return wrapPromise(vm, func() (any, error) {
					id := extractString(call, 0)
					if err := storageService.Delete(storageCtx, id); err != nil {
						return nil, err
					}
					return goja.Undefined(), nil
				})
			}))
		}
		obj.Set("storage", storageObj)
		return nil
	}
}

func extractImageUploadOptions(call goja.FunctionCall) (map[string]string, error) {
	if len(call.Arguments) == 0 || goja.IsUndefined(call.Argument(0)) {
		return nil, nil
	}
	options, ok := call.Argument(0).Export().(map[string]any)
	if !ok || len(options) != 2 {
		return nil, fmt.Errorf("generateUploadUrl options must contain table and field")
	}
	table, tableOK := options["table"].(string)
	field, fieldOK := options["field"].(string)
	if !tableOK || !fieldOK || table == "" || field == "" {
		return nil, fmt.Errorf("generateUploadUrl table and field must be strings")
	}
	return map[string]string{"table": table, "field": field}, nil
}

func imagePolicyForField(manifest deploy.DeploymentManifest, modulePath, tableName, fieldName string) (storage.ImagePolicy, error) {
	deployedSchema := manifest.Schema
	if namespace, ok := deploy.NamespaceForModule(manifest, modulePath); ok {
		deployedSchema = namespace.Schema
	}
	schemaObject, ok := deployedSchema.(map[string]any)
	if !ok {
		return storage.ImagePolicy{}, fmt.Errorf("deployment has no schema")
	}
	tables, ok := schemaObject["tables"].([]any)
	if !ok {
		return storage.ImagePolicy{}, fmt.Errorf("deployment schema is invalid")
	}
	for _, rawTable := range tables {
		table, ok := rawTable.(map[string]any)
		if !ok || table["tableName"] != tableName {
			continue
		}
		fields, ok := table["fields"].(map[string]any)
		if !ok {
			break
		}
		descriptor, ok := fields[fieldName].(map[string]any)
		if !ok {
			break
		}
		for descriptor["type"] == "optional" || descriptor["type"] == "defaulted" {
			descriptor, ok = descriptor["validator"].(map[string]any)
			if !ok {
				break
			}
		}
		if descriptor["type"] != "image" {
			break
		}
		thumbs, thumbsOK := stringSlice(descriptor["thumbs"])
		mimeTypes, mimeOK := stringSlice(descriptor["mimeTypes"])
		if !thumbsOK || !mimeOK {
			break
		}
		return storage.ImagePolicy{Kind: "image", Thumbs: thumbs, MimeTypes: mimeTypes}, nil
	}
	return storage.ImagePolicy{}, fmt.Errorf("schema field %s.%s is not an image", tableName, fieldName)
}

func stringSlice(value any) ([]string, bool) {
	raw, ok := value.([]any)
	if !ok {
		return nil, false
	}
	result := make([]string, len(raw))
	for i, value := range raw {
		item, ok := value.(string)
		if !ok {
			return nil, false
		}
		result[i] = item
	}
	return result, true
}

func extractStorageURLMode(vm *goja.Runtime, call goja.FunctionCall) (string, error) {
	if len(call.Arguments) < 2 || goja.IsUndefined(call.Argument(1)) {
		return "identity", nil
	}
	value := call.Argument(1)
	if goja.IsNull(value) {
		return "", fmt.Errorf("storage getUrl options must be an object")
	}
	obj := value.ToObject(vm)
	if obj.ClassName() != "Object" {
		return "", fmt.Errorf("storage getUrl options must be an object")
	}
	keys := obj.Keys()
	if len(keys) != 1 || keys[0] != "mode" {
		return "", fmt.Errorf("storage getUrl options only support mode")
	}
	mode, ok := obj.Get("mode").Export().(string)
	if !ok || (mode != "identity" && mode != "capability" && mode != "public") {
		return "", fmt.Errorf("storage getUrl mode must be identity, capability, or public")
	}
	return mode, nil
}

func extractString(call goja.FunctionCall, idx int) string {
	if idx >= len(call.Arguments) {
		return ""
	}
	arg := call.Argument(idx)
	if goja.IsUndefined(arg) || goja.IsNull(arg) {
		return ""
	}
	return arg.String()
}

func wrapPromise(vm *goja.Runtime, fn func() (any, error)) goja.Value {
	promise, resolve, reject := vm.NewPromise()
	result, err := fn()
	if err != nil {
		_ = reject(err.Error())
		return vm.ToValue(promise)
	}
	var val goja.Value
	if result == nil {
		val = goja.Undefined()
	} else if v, ok := result.(goja.Value); ok {
		val = v
	} else {
		val = vm.ToValue(result)
	}
	if err := resolve(val); err != nil {
		_ = reject(fmt.Sprintf("resolve: %v", err))
	}
	return vm.ToValue(promise)
}
