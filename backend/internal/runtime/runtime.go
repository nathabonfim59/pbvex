package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja_nodejs/eventloop"
	"github.com/nathabonfim59/pbvex/backend/internal/auth"
	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/pocketbase/pocketbase/core"
)

// Config controls the Goja runtime pool.
type Config struct {
	PoolSize int
	Timeout  time.Duration
}

// DefaultConfig returns the default runtime pool configuration.
func DefaultConfig() Config {
	return Config{
		PoolSize: 5,
		Timeout:  30 * time.Second,
	}
}

// RuntimeInvoker is the interface used by the deploy service.
type RuntimeInvoker interface {
	Compile(deploymentID, bundle string, descriptors []deploy.FunctionDescriptor, config ...deploy.DeploymentConfig) error
	Verify(ctx context.Context, deploymentID, bundle string, descriptors []deploy.FunctionDescriptor) error
	Invoke(ctx context.Context, deploymentID, functionName string, args any, authArgs ...any) (any, error)
	InvokeHTTP(ctx context.Context, deploymentID, functionName string, httpEnvelope *deploy.HTTPRequestEnvelope, identity *auth.UserIdentity, requestID string) (*deploy.HTTPResponseEnvelope, error)
}

const maxMigrationFailureMessage = 1024

// MigrationError is the bounded, document-free failure produced by ctx.fail.
type MigrationError struct{ Message string }

func (e *MigrationError) Error() string { return "migration failed: " + e.Message }

// Scheduler is the capability exposed to mutations and actions.
type Scheduler interface {
	RunAfter(ctx context.Context, delayMs int64, deploymentID, functionName string, args any) (string, error)
	RunAt(ctx context.Context, epochMs int64, deploymentID, functionName string, args any) (string, error)
	Cancel(ctx context.Context, jobID string) error
}

// Manager is a registry of bounded Goja runtime pools keyed by deployment id.
type Manager struct {
	config    Config
	mu        sync.RWMutex
	pools     map[string]*Pool
	Scheduler Scheduler
	extenders []ContextExtender
}

// NewManager creates a new runtime manager.
func NewManager(config Config) *Manager {
	if config.PoolSize <= 0 {
		config.PoolSize = DefaultConfig().PoolSize
	}
	if config.Timeout <= 0 {
		config.Timeout = DefaultConfig().Timeout
	}
	return &Manager{
		config: config,
		pools:  make(map[string]*Pool),
	}
}

// Compile compiles and stores the bundle program for a deployment.
func (m *Manager) Compile(deploymentID, bundle string, descriptors []deploy.FunctionDescriptor, configs ...deploy.DeploymentConfig) error {
	return m.CompileDeployment(deploymentID, bundle, descriptors, nil, configs...)
}

// CompileDeployment stores both function and migration registration contracts.
func (m *Manager) CompileDeployment(deploymentID, bundle string, descriptors []deploy.FunctionDescriptor, migrations []deploy.MigrationDescriptor, configs ...deploy.DeploymentConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	config := deploy.DefaultDeploymentConfig
	if len(configs) > 0 {
		config = deploy.NormalizeConfig(&configs[0])
	}

	fingerprint := runtimeFingerprint(bundle, descriptors, migrations, config, m.config)
	if existing, ok := m.pools[deploymentID]; ok && existing.fingerprint == fingerprint {
		return nil
	}

	program, err := goja.Compile("bundle.js", bundle, false)
	if err != nil {
		return fmt.Errorf("invalid bundle: %w", err)
	}

	extenders := append([]ContextExtender(nil), m.extenders...)
	m.pools[deploymentID] = newPool(m.config.PoolSize, m.config.Timeout, program, descriptors, migrations, config, fingerprint, m.Scheduler, deploymentID, extenders)
	return nil
}

// Drop removes an invalidated deployment runtime (trim, deletion, rollback
// transitions). It is safe for in-flight callers: they hold the old pool.
func (m *Manager) Drop(deploymentID string) {
	m.mu.Lock()
	delete(m.pools, deploymentID)
	m.mu.Unlock()
}

func runtimeFingerprint(bundle string, descriptors []deploy.FunctionDescriptor, migrations []deploy.MigrationDescriptor, cfg deploy.DeploymentConfig, config Config) string {
	h := sha256.New()
	h.Write([]byte(bundle))
	for _, d := range descriptors {
		s, _ := deploy.CanonicalJSON(descriptorJSON(d))
		h.Write([]byte(s))
	}
	for _, d := range migrations {
		s, _ := deploy.CanonicalJSON(migrationDescriptorJSON(d))
		h.Write([]byte(s))
	}
	cfgJSON, _ := deploy.CanonicalJSON(deploymentConfigJSON(cfg))
	h.Write([]byte(cfgJSON))
	h.Write([]byte(fmt.Sprintf("%d/%d", config.PoolSize, config.Timeout)))
	return fmt.Sprintf("%x", h.Sum(nil))
}

func deploymentConfigJSON(cfg deploy.DeploymentConfig) map[string]any {
	return map[string]any{
		"httpPathPrefix":          cfg.HTTPPathPrefix,
		"realtimePath":            cfg.RealtimePath,
		"maxUploadBytes":          cfg.MaxUploadBytes,
		"maxFunctionArgsBytes":    cfg.MaxFunctionArgsBytes,
		"maxReturnValueBytes":     cfg.MaxReturnValueBytes,
		"defaultRequestTimeoutMs": cfg.DefaultRequestTimeoutMs,
	}
}

// Verify loads the bundle in a fresh runtime and confirms that every declared
// function is registered with an exact descriptor match.
func (m *Manager) Verify(ctx context.Context, deploymentID, bundle string, descriptors []deploy.FunctionDescriptor) error {
	return m.VerifyDeployment(ctx, deploymentID, bundle, descriptors, nil)
}

// VerifyDeployment requires exact function and migration registration parity.
func (m *Manager) VerifyDeployment(ctx context.Context, deploymentID, bundle string, descriptors []deploy.FunctionDescriptor, migrations []deploy.MigrationDescriptor) error {
	program, err := goja.Compile("bundle.js", bundle, false)
	if err != nil {
		return fmt.Errorf("invalid bundle: %w", err)
	}

	ctx, cancel := contextWithTimeout(ctx, m.config.Timeout)
	defer cancel()

	e := newEntry(program, descriptors, migrations, deploy.DefaultDeploymentConfig)

	if err := e.load(ctx, program); err != nil {
		return err
	}
	return e.bridge.Verify(descriptors, migrations)
}

// InvokeMigration executes a pure synchronous up/down handler. The caller owns
// the surrounding database transaction; this method never starts one.
func (m *Manager) InvokeMigration(ctx context.Context, deploymentID, migrationID, direction string, document any, activationTime int64) (any, error) {
	m.mu.RLock()
	pool, ok := m.pools[deploymentID]
	m.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("deployment %s runtime not compiled", deploymentID)
	}
	ctx, cancel := contextWithTimeout(ctx, m.config.Timeout)
	defer cancel()
	e, err := pool.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer pool.release()
	return e.invokeMigration(ctx, migrationID, direction, document, activationTime)
}

// Invoke runs the named function in a fresh bounded runtime.
func (m *Manager) Invoke(ctx context.Context, deploymentID, functionName string, args any, authArgs ...any) (any, error) {
	m.mu.RLock()
	pool, ok := m.pools[deploymentID]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("deployment %s runtime not compiled", deploymentID)
	}

	ctx, cancel := contextWithTimeout(ctx, m.config.Timeout)
	defer cancel()

	identity, requestID := invocationMetadata(ctx, authArgs)
	ctx = withRuntimeAuth(ctx, identity, requestID)
	invocation := &Invocation{
		Ctx:            ctx,
		Identity:       identity,
		RequestID:      requestID,
		DeploymentID:   deploymentID,
		MaxArgsBytes:   pool.config.MaxFunctionArgsBytes,
		MaxReturnBytes: pool.config.MaxReturnValueBytes,
		RequestTimeout: pool.timeout,
		Work:           new(int),
	}

	return pool.invoke(invocation, functionName, args)
}

// InvokeWithDatabase is used by deploy.Service for real requests that need
// database access. Mutations are wrapped in a PocketBase transaction so that
// invalid returns, timeouts, and cancellations roll back all writes.
func (m *Manager) InvokeWithDatabase(ctx context.Context, deploymentID, functionName string, args any, extra ...any) (any, error) {
	m.mu.RLock()
	pool, ok := m.pools[deploymentID]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("deployment %s runtime not compiled", deploymentID)
	}

	ctx, cancel := contextWithTimeout(ctx, m.config.Timeout)
	defer cancel()

	identity, requestID, app, manifest, err := databaseInvocationArgs(ctx, extra)
	if err != nil {
		return nil, err
	}
	ctx = withRuntimeAuth(ctx, identity, requestID)
	cfg := deploy.NormalizeConfig(manifest.Config)
	invocation := &Invocation{
		Ctx:            ctx,
		Identity:       identity,
		RequestID:      requestID,
		DeploymentID:   deploymentID,
		MaxArgsBytes:   cfg.MaxFunctionArgsBytes,
		MaxReturnBytes: cfg.MaxReturnValueBytes,
		RequestTimeout: pool.timeout,
		Work:           new(int),
		App:            app,
		Manifest:       manifest,
	}
	if app != nil {
		invocation.Ctx = schema.WithApp(ctx, app)
	}

	if app != nil && pool.isMutation(functionName) {
		var result any
		err := app.RunInTransaction(func(txApp core.App) error {
			invocation.App = txApp
			invocation.Ctx = schema.WithApp(ctx, txApp)
			var invokeErr error
			result, invokeErr = pool.invoke(invocation, functionName, args)
			if invokeErr == nil && ctx.Err() != nil {
				invokeErr = ctx.Err()
			}
			return invokeErr
		})
		if err != nil {
			return nil, err
		}
		return result, nil
	}

	return pool.invoke(invocation, functionName, args)
}

func invocationMetadata(ctx context.Context, extra []any) (*auth.UserIdentity, string) {
	metadata := auth.InvocationMetadataFromContext(ctx)
	identity := metadata.Identity
	requestID := metadata.RequestID
	if authCtx, ok := AuthFromContext(ctx); ok {
		identity = authCtx.Identity
		requestID = authCtx.RequestID
	}
	if len(extra) >= 1 {
		identity, _ = extra[0].(*auth.UserIdentity)
	}
	if len(extra) >= 2 {
		requestID, _ = extra[1].(string)
	}
	return identity, requestID
}

func withRuntimeAuth(ctx context.Context, identity *auth.UserIdentity, requestID string) context.Context {
	if identity == nil {
		return WithAuthContext(ctx, AuthContext{RequestID: requestID})
	}
	return WithAuthContext(ctx, AuthContext{IsAuthenticated: true, TokenIdentifier: identity.TokenIdentifier, Identity: identity, RequestID: requestID})
}

func databaseInvocationArgs(ctx context.Context, extra []any) (*auth.UserIdentity, string, core.App, deploy.DeploymentManifest, error) {
	identity, requestID := invocationMetadata(ctx, nil)
	var app core.App
	var manifest deploy.DeploymentManifest
	switch len(extra) {
	case 2:
		app, _ = extra[0].(core.App)
		manifest, _ = extra[1].(deploy.DeploymentManifest)
	case 4:
		identity, _ = extra[0].(*auth.UserIdentity)
		requestID, _ = extra[1].(string)
		app, _ = extra[2].(core.App)
		manifest, _ = extra[3].(deploy.DeploymentManifest)
	default:
		return nil, "", nil, deploy.DeploymentManifest{}, fmt.Errorf("invalid database invocation metadata")
	}
	return identity, requestID, app, manifest, nil
}

// InvokeHTTP runs the named httpAction and returns an HTTP response envelope.
func (m *Manager) InvokeHTTP(ctx context.Context, deploymentID, functionName string, httpEnvelope *deploy.HTTPRequestEnvelope, identity *auth.UserIdentity, requestID string) (*deploy.HTTPResponseEnvelope, error) {
	return m.InvokeHTTPWithDatabase(ctx, deploymentID, functionName, httpEnvelope, identity, requestID, nil, deploy.DeploymentManifest{})
}

// InvokeHTTPWithDatabase preserves the app and manifest snapshot for nested
// calls issued by an HTTP action.
func (m *Manager) InvokeHTTPWithDatabase(ctx context.Context, deploymentID, functionName string, httpEnvelope *deploy.HTTPRequestEnvelope, identity *auth.UserIdentity, requestID string, app core.App, manifest deploy.DeploymentManifest) (*deploy.HTTPResponseEnvelope, error) {
	m.mu.RLock()
	pool, ok := m.pools[deploymentID]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("deployment %s runtime not compiled", deploymentID)
	}

	ctx = withRuntimeAuth(ctx, identity, requestID)
	ctx, cancel := contextWithTimeout(ctx, m.config.Timeout)
	defer cancel()

	invocation := &Invocation{
		Ctx:            ctx,
		Identity:       identity,
		RequestID:      requestID,
		DeploymentID:   deploymentID,
		FunctionType:   deploy.FunctionTypeHTTPAction,
		HTTPRequest:    httpEnvelope,
		MaxArgsBytes:   pool.config.MaxFunctionArgsBytes,
		MaxReturnBytes: pool.config.MaxReturnValueBytes,
		RequestTimeout: pool.timeout,
		Work:           new(int),
		App:            app,
		Manifest:       manifest,
	}
	if app != nil {
		invocation.Ctx = schema.WithApp(ctx, app)
	}

	return pool.invokeHTTP(invocation, functionName, httpEnvelope)
}

func contextWithTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, timeout)
}

// Pool is a bounded concurrency gate for Goja runtimes for a single deployment.
type Pool struct {
	program      *goja.Program
	descriptors  []deploy.FunctionDescriptor
	migrations   []deploy.MigrationDescriptor
	config       deploy.DeploymentConfig
	timeout      time.Duration
	sem          chan struct{}
	fingerprint  string
	scheduler    Scheduler
	deploymentID string
	extenders    []ContextExtender
}

func newPool(maxSize int, timeout time.Duration, program *goja.Program, descriptors []deploy.FunctionDescriptor, migrations []deploy.MigrationDescriptor, config deploy.DeploymentConfig, fingerprint string, scheduler Scheduler, deploymentID string, extenders []ContextExtender) *Pool {
	return &Pool{
		program:      program,
		descriptors:  descriptors,
		migrations:   migrations,
		config:       config,
		timeout:      timeout,
		sem:          make(chan struct{}, maxSize),
		fingerprint:  fingerprint,
		scheduler:    scheduler,
		deploymentID: deploymentID,
		extenders:    extenders,
	}
}

func (p *Pool) acquire(ctx context.Context) (*entry, error) {
	select {
	case p.sem <- struct{}{}:
		return newEntry(p.program, p.descriptors, p.migrations, p.config, p.scheduler, p.deploymentID, p.extenders), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *Pool) release() {
	<-p.sem
}

func (p *Pool) isMutation(functionName string) bool {
	for _, d := range p.descriptors {
		if d.Name == functionName {
			return d.Type == deploy.FunctionTypeMutation
		}
	}
	return false
}

func (p *Pool) invoke(invocation *Invocation, functionName string, args any) (any, error) {
	e, err := p.acquire(invocation.Ctx)
	if err != nil {
		return nil, err
	}
	defer p.release()

	invocation.NestedInvoke = p.invokeNested
	return e.invoke(invocation, functionName, args)
}

func (p *Pool) descriptor(functionName string) (deploy.FunctionDescriptor, bool) {
	for _, descriptor := range p.descriptors {
		if descriptor.Name == functionName {
			return descriptor, true
		}
	}
	return deploy.FunctionDescriptor{}, false
}

// invokeNested intentionally bypasses the pool semaphore. The parent already
// owns a slot, and every nested call receives an isolated runtime entry.
func (p *Pool) invokeNested(parent *Invocation, functionName string, targetType deploy.FunctionType, args any, depth int) (any, error) {
	descriptor, ok := p.descriptor(functionName)
	if !ok {
		return nil, fmt.Errorf("function not found")
	}
	if descriptor.Type != targetType {
		return nil, fmt.Errorf("function %q is not a %s", functionName, targetType)
	}
	child := &Invocation{
		Ctx: parent.Ctx, Identity: parent.Identity, RequestID: parent.RequestID,
		DeploymentID: parent.DeploymentID, FunctionType: targetType, FunctionName: functionName,
		App: parent.App, Manifest: parent.Manifest, MaxArgsBytes: parent.MaxArgsBytes,
		MaxReturnBytes: parent.MaxReturnBytes, RequestTimeout: parent.RequestTimeout,
		Depth: depth, Work: parent.Work, NestedInvoke: p.invokeNested,
	}
	child.Namespace = namespaceForDescriptor(parent.Manifest, descriptor)
	invoke := func() (any, error) {
		e := newEntry(p.program, p.descriptors, p.migrations, p.config, p.scheduler, p.deploymentID, p.extenders)
		return e.invoke(child, functionName, args)
	}
	if targetType == deploy.FunctionTypeMutation && parent.FunctionType != deploy.FunctionTypeMutation && parent.App != nil {
		var result any
		err := parent.App.RunInTransaction(func(txApp core.App) error {
			child.App = txApp
			child.Ctx = schema.WithApp(parent.Ctx, txApp)
			var invokeErr error
			result, invokeErr = invoke()
			if invokeErr == nil && child.Ctx.Err() != nil {
				invokeErr = child.Ctx.Err()
			}
			return invokeErr
		})
		return result, err
	}
	return invoke()
}

func (p *Pool) invokeHTTP(invocation *Invocation, functionName string, httpEnvelope *deploy.HTTPRequestEnvelope) (*deploy.HTTPResponseEnvelope, error) {
	e, err := p.acquire(invocation.Ctx)
	if err != nil {
		return nil, err
	}
	defer p.release()

	invocation.NestedInvoke = p.invokeNested
	return e.invokeHTTP(invocation, functionName, httpEnvelope)
}

func (p *Pool) verify(ctx context.Context, descriptors []deploy.FunctionDescriptor) error {
	e, err := p.acquire(ctx)
	if err != nil {
		return err
	}
	defer p.release()

	if err := e.load(ctx, p.program); err != nil {
		return err
	}

	return e.bridge.Verify(descriptors, p.migrations)
}

type entry struct {
	loop              *eventloop.EventLoop
	vm                *goja.Runtime
	bridge            *Bridge
	loaded            bool
	program           *goja.Program
	invocation        *Invocation
	promiseResolve    goja.Callable
	promiseCtor       *goja.Object
	uint8ArrayCtor    goja.Constructor
	scheduler         Scheduler
	deploymentID      string
	extenders         []ContextExtender
	applicationErrors map[*goja.Object]registeredApplicationError
}

type registeredApplicationError struct {
	category deploy.ApplicationErrorCategory
	data     goja.Value
	hasData  bool
}

func newEntry(program *goja.Program, descriptors []deploy.FunctionDescriptor, args ...any) *entry {
	var migrations []deploy.MigrationDescriptor
	extra := args
	if len(args) > 0 {
		if value, ok := args[0].([]deploy.MigrationDescriptor); ok {
			migrations = value
			extra = args[1:]
		}
	}
	loop := eventloop.NewEventLoop(eventloop.EnableConsole(false))
	bridge := &Bridge{
		handlers:             make(map[string]goja.Callable),
		descriptors:          make(map[string]deploy.FunctionDescriptor),
		descriptorsByPath:    make(map[string]deploy.FunctionDescriptor),
		manifestDescriptors:  descriptors,
		migrationHandlers:    make(map[string]migrationHandlers),
		migrationDescriptors: make(map[string]deploy.MigrationDescriptor),
		manifestMigrations:   migrations,
	}
	e := &entry{
		loop:              loop,
		vm:                goja.New(),
		bridge:            bridge,
		program:           program,
		applicationErrors: make(map[*goja.Object]registeredApplicationError),
	}
	if len(extra) >= 4 {
		e.scheduler, _ = extra[1].(Scheduler)
		e.deploymentID, _ = extra[2].(string)
		e.extenders, _ = extra[3].([]ContextExtender)
	} else if len(extra) >= 3 {
		e.scheduler, _ = extra[0].(Scheduler)
		e.deploymentID, _ = extra[1].(string)
		e.extenders, _ = extra[2].([]ContextExtender)
	}
	return e
}

func (e *entry) startTimer(ctx context.Context, vm *goja.Runtime) func() bool {
	if ctx == nil {
		return nil
	}
	return context.AfterFunc(ctx, func() {
		vm.Interrupt(ctx.Err())
	})
}

func (e *entry) load(ctx context.Context, program *goja.Program) error {
	if e.loaded || program == nil {
		return nil
	}
	var err error
	var stop func() bool
	e.loop.Run(func(vm *goja.Runtime) {
		e.vm = vm
		stop = e.startTimer(ctx, vm)
		err = e.loadInLoop(vm, program)
	})
	if stop != nil {
		stop()
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func (e *entry) loadInLoop(vm *goja.Runtime, program *goja.Program) error {
	if e.loaded || program == nil {
		return nil
	}
	e.vm = vm

	if err := e.loadWebGlobals(vm); err != nil {
		return fmt.Errorf("failed to load web globals: %w", err)
	}

	promiseObj := vm.Get("Promise").ToObject(vm)
	resolve, ok := goja.AssertFunction(promiseObj.Get("resolve"))
	if !ok {
		return fmt.Errorf("Promise.resolve not found")
	}
	e.promiseResolve = resolve
	e.promiseCtor = promiseObj

	_ = vm.Set("__pbvex", map[string]any{
		"registerFunction":       e.bridge.RegisterFunction,
		"registerMigration":      e.bridge.RegisterMigration,
		"createApplicationError": e.createApplicationError,
	})

	if _, err := vm.RunProgram(program); err != nil {
		return fmt.Errorf("failed to load bundle: %w", err)
	}
	e.loaded = true
	return nil
}

func (e *entry) invoke(invocation *Invocation, functionName string, args any) (any, error) {
	var raw goja.Value
	var err error
	var stop func() bool
	e.loop.Run(func(vm *goja.Runtime) {
		e.vm = vm
		stop = e.startTimer(invocation.Ctx, vm)
		if err = e.loadInLoop(vm, e.program); err != nil {
			return
		}
		raw, err = e.invokeRaw(invocation, functionName, args)
	})
	if stop != nil {
		stop()
	}
	if invocation.Ctx.Err() != nil {
		return nil, invocation.Ctx.Err()
	}
	if err != nil {
		return nil, err
	}
	return e.encodeResult(raw, invocation)
}

func (e *entry) invokeHTTP(invocation *Invocation, functionName string, httpEnvelope *deploy.HTTPRequestEnvelope) (*deploy.HTTPResponseEnvelope, error) {
	var raw goja.Value
	var err error
	var stop func() bool
	e.loop.Run(func(vm *goja.Runtime) {
		e.vm = vm
		stop = e.startTimer(invocation.Ctx, vm)
		if err = e.loadInLoop(vm, e.program); err != nil {
			return
		}
		raw, err = e.invokeHTTPRaw(invocation, functionName, httpEnvelope)
	})
	if stop != nil {
		stop()
	}
	if invocation.Ctx.Err() != nil {
		return nil, invocation.Ctx.Err()
	}
	if err != nil {
		if applicationErr := e.applicationErrorFromThrown(err, invocation); applicationErr != nil {
			return nil, applicationErr
		}
		return nil, err
	}
	return e.toHTTPResponse(raw, invocation)
}

func (e *entry) invokeMigration(ctx context.Context, migrationID, direction string, document any, activationTime int64) (any, error) {
	var raw goja.Value
	var err error
	var stop func() bool
	e.loop.Run(func(vm *goja.Runtime) {
		e.vm = vm
		stop = e.startTimer(ctx, vm)
		if err = e.loadInLoop(vm, e.program); err != nil {
			return
		}
		handlers, ok := e.bridge.migrationHandlers[migrationID]
		if !ok {
			err = fmt.Errorf("migration is not registered")
			return
		}
		fn := handlers.up
		if direction == "down" {
			fn = handlers.down
		} else if direction != "up" {
			err = fmt.Errorf("migration direction is invalid")
			return
		}
		doc, decodeErr := decodeWire(vm, document)
		if decodeErr != nil {
			err = fmt.Errorf("migration input is invalid")
			return
		}
		migrationCtx := vm.NewObject()
		_ = migrationCtx.Set("migrationId", migrationID)
		_ = migrationCtx.Set("activationTime", activationTime)
		_ = migrationCtx.Set("fail", func(call goja.FunctionCall) goja.Value {
			message := call.Argument(0).String()
			if len(message) > maxMigrationFailureMessage {
				message = message[:maxMigrationFailureMessage]
			}
			panic(vm.NewGoError(&MigrationError{Message: message}))
		})
		raw, err = fn(goja.Undefined(), doc, migrationCtx)
	})
	if stop != nil {
		stop()
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		return nil, fmt.Errorf("migration handler failed")
	}
	if raw == nil || goja.IsNull(raw) || goja.IsUndefined(raw) {
		return nil, fmt.Errorf("migration output must be an object")
	}
	if _, promise := raw.Export().(*goja.Promise); promise {
		return nil, fmt.Errorf("migration handler must be synchronous")
	}
	encoded, err := encodeWire(e.vm, raw)
	if err != nil {
		return nil, fmt.Errorf("migration output is invalid")
	}
	if _, ok := encoded.(map[string]any); !ok {
		return nil, fmt.Errorf("migration output must be an object")
	}
	return encoded, nil
}

func (e *entry) invokeRaw(invocation *Invocation, functionName string, args any) (goja.Value, error) {
	fn := e.bridge.handlers[functionName]
	if fn == nil {
		return nil, fmt.Errorf("function %q not registered", functionName)
	}
	descriptor := e.bridge.descriptors[functionName]

	if invocation == nil {
		invocation = &Invocation{}
	}
	invocation.FunctionType = descriptor.Type
	invocation.FunctionName = functionName
	invocation.Namespace = namespaceForDescriptor(invocation.Manifest, descriptor)
	e.invocation = invocation

	normalizedArgs := args
	if descriptor.Args != nil {
		var check func(string, string) bool
		if invocation.App != nil {
			check = databaseScope(invocation.Ctx, invocation.App, invocation.Manifest, descriptor).validIDForTable
		}
		var normalizeErr error
		normalizedArgs, normalizeErr = normalizeValueWithID(descriptor.Args, args, check)
		if normalizeErr != nil {
			return nil, fmt.Errorf("invalid function arguments: %w", normalizeErr)
		}
	}

	argsValue, err := decodeWire(e.vm, normalizedArgs)
	if err != nil {
		return nil, fmt.Errorf("invalid function arguments: %w", err)
	}

	encoded, err := encodeWire(e.vm, argsValue)
	if err != nil {
		return nil, fmt.Errorf("invalid function arguments: %w", err)
	}
	if err := checkWireSize(encoded, invocation.MaxArgsBytes, "function arguments"); err != nil {
		return nil, err
	}

	jsArgs, err := decodeWire(e.vm, encoded)
	if err != nil {
		return nil, fmt.Errorf("invalid function arguments: %w", err)
	}
	if goja.IsNull(jsArgs) {
		jsArgs = goja.Undefined()
	}

	ctx, err := e.buildInvocationContext(invocation, descriptor, functionName, normalizedArgs, jsArgs)
	if err != nil {
		return nil, err
	}
	val, err := fn(goja.Undefined(), ctx, jsArgs)
	if err != nil {
		if applicationErr := e.applicationErrorFromThrown(err, invocation); applicationErr != nil {
			return nil, applicationErr
		}
		return nil, err
	}
	return val, nil
}

func (e *entry) invokeHTTPRaw(invocation *Invocation, functionName string, httpEnvelope *deploy.HTTPRequestEnvelope) (goja.Value, error) {
	fn := e.bridge.handlers[functionName]
	if fn == nil {
		return nil, fmt.Errorf("function %q not registered", functionName)
	}
	descriptor := e.bridge.descriptors[functionName]
	if descriptor.Type != deploy.FunctionTypeHTTPAction {
		return nil, fmt.Errorf("function %q is not an httpAction", functionName)
	}

	if invocation == nil {
		invocation = &Invocation{}
	}
	invocation.FunctionType = deploy.FunctionTypeHTTPAction
	invocation.HTTPRequest = httpEnvelope
	e.invocation = invocation

	if httpEnvelope == nil {
		return nil, fmt.Errorf("httpAction invocation missing HTTP request")
	}
	if int64(len(httpEnvelope.Body)) > invocation.MaxArgsBytes {
		return nil, fmt.Errorf("httpAction request body exceeds configured limit")
	}

	request, err := e.buildRequest(httpEnvelope)
	if err != nil {
		return nil, fmt.Errorf("failed to build Request object: %w", err)
	}

	ctx, err := e.buildInvocationContext(invocation, descriptor, functionName, nil, goja.Undefined())
	if err != nil {
		return nil, err
	}
	return fn(goja.Undefined(), ctx, request)
}

func (e *entry) createApplicationError(call goja.FunctionCall) goja.Value {
	category := deploy.ApplicationErrorCategory(call.Argument(0).String())
	if !deploy.IsApplicationErrorCategory(category) {
		return e.vm.NewGoError(fmt.Errorf("invalid application error category"))
	}
	obj := e.vm.NewGoError(fmt.Errorf("application error: %s", category))
	_ = obj.Set("name", "ApplicationError")
	_ = obj.Set("category", string(category))
	_ = obj.Set("data", call.Argument(1))
	if prototype, ok := call.Argument(3).(*goja.Object); ok {
		_ = obj.SetPrototype(prototype)
	}
	registered := registeredApplicationError{category: category}
	if call.Argument(2).ToBoolean() {
		registered.data = call.Argument(1)
		registered.hasData = true
	}
	e.applicationErrors[obj] = registered
	return obj
}

func (e *entry) applicationErrorFromThrown(thrown any, invocation *Invocation) *deploy.ApplicationError {
	var value goja.Value
	switch typed := thrown.(type) {
	case goja.Value:
		value = typed
	case interface{ Value() goja.Value }:
		value = typed.Value()
	}
	obj, ok := value.(*goja.Object)
	if !ok {
		return nil
	}
	registered, ok := e.applicationErrors[obj]
	if !ok || !deploy.IsApplicationErrorCategory(registered.category) {
		return nil
	}
	result := &deploy.ApplicationError{Category: registered.category, HasData: registered.hasData}
	if registered.hasData {
		encoded, err := encodeWire(e.vm, registered.data)
		if err != nil || checkWireSize(encoded, invocation.MaxReturnBytes, "application error data") != nil {
			return nil
		}
		result.Data = encoded
	}
	return result
}

func (e *entry) throwApplicationError(applicationErr *deploy.ApplicationError) {
	obj := e.vm.NewGoError(applicationErr)
	registered := registeredApplicationError{category: applicationErr.Category, hasData: applicationErr.HasData}
	if applicationErr.HasData {
		data, err := decodeWire(e.vm, applicationErr.Data)
		if err == nil {
			registered.data = data
		} else {
			panic(e.vm.NewGoError(fmt.Errorf("invalid nested application error")))
		}
	}
	e.applicationErrors[obj] = registered
	panic(obj)
}

func (e *entry) buildInvocationContext(invocation *Invocation, descriptor deploy.FunctionDescriptor, functionName string, normalizedArgs any, jsArgs goja.Value) (*goja.Object, error) {
	ctx, err := newInvocationContext(e.vm, invocation.Ctx, invocation.App, invocation.Manifest, descriptor, functionName, normalizedArgs, jsArgs, e.extenders)
	if err != nil {
		return nil, err
	}
	if err := ctx.Set("auth", e.makeAuthObject(invocation.Identity)); err != nil {
		return nil, err
	}
	if descriptor.Type == deploy.FunctionTypeMutation || descriptor.Type == deploy.FunctionTypeAction || descriptor.Type == deploy.FunctionTypeHTTPAction {
		if e.scheduler != nil {
			if err := e.bindScheduler(ctx, invocation); err != nil {
				return nil, err
			}
		}
	}
	if descriptor.Type == deploy.FunctionTypeAction || descriptor.Type == deploy.FunctionTypeHTTPAction {
		depth := invocation.Depth + 1
		_ = ctx.Set("run", e.makeRun(depth))
		_ = ctx.Set("runQuery", e.makeRunQuery(depth))
		_ = ctx.Set("runMutation", e.makeRunMutation(depth))
		_ = ctx.Set("runAction", e.makeRunAction(depth))
	}
	return ctx, nil
}

func (e *entry) buildRequest(httpEnvelope *deploy.HTTPRequestEnvelope) (goja.Value, error) {
	if err := deploy.ValidateHTTPHeaders(httpEnvelope.Headers); err != nil {
		return nil, fmt.Errorf("invalid HTTP request headers: %w", err)
	}
	requestCtor, ok := goja.AssertConstructor(e.vm.Get("Request"))
	if !ok {
		return nil, fmt.Errorf("Request is not a constructor")
	}

	httpHeaders := httpEnvelope.Headers
	if httpHeaders == nil {
		httpHeaders = map[string][]string{}
	}
	pairValues := make([]interface{}, 0, len(httpHeaders))
	for name, values := range httpHeaders {
		for _, value := range values {
			pair := e.vm.NewArray(e.vm.ToValue(name), e.vm.ToValue(value))
			pairValues = append(pairValues, pair)
		}
	}
	headersArr := e.vm.NewArray(pairValues...)
	headersCtor, ok := goja.AssertConstructor(e.vm.Get("Headers"))
	if !ok {
		return nil, fmt.Errorf("Headers is not a constructor")
	}
	headersObj, err := headersCtor(nil, headersArr)
	if err != nil {
		return nil, err
	}

	init := e.vm.NewObject()
	_ = init.Set("method", e.vm.ToValue(httpEnvelope.Method))
	_ = init.Set("headers", headersObj)
	if len(httpEnvelope.Body) > 0 {
		_ = init.Set("body", e.vm.NewArrayBuffer(httpEnvelope.Body))
	}

	return requestCtor(nil, e.vm.ToValue(httpEnvelope.URL), init)
}

func (e *entry) encodeResult(val goja.Value, invocation *Invocation) (any, error) {
	resolved, err := e.resolveValue(val, invocation.Ctx)
	if err != nil {
		return nil, err
	}
	encoded, err := encodeWire(e.vm, resolved)
	if err != nil {
		return nil, fmt.Errorf("invalid function return value: %w", err)
	}
	if descriptor, ok := e.bridge.descriptors[invocation.FunctionName]; ok && descriptor.Returns != nil {
		var check func(string, string) bool
		if invocation.App != nil {
			check = databaseScope(invocation.Ctx, invocation.App, invocation.Manifest, descriptor).validIDForTable
		}
		encoded, err = normalizeValueWithID(descriptor.Returns, encoded, check)
		if err != nil {
			return nil, fmt.Errorf("invalid function return value: %w", err)
		}
	}
	if err := checkWireSize(encoded, invocation.MaxReturnBytes, "function return value"); err != nil {
		return nil, err
	}
	return encoded, nil
}

func (e *entry) resolveValue(val goja.Value, ctx context.Context) (goja.Value, error) {
	for {
		if val == nil || goja.IsNull(val) || goja.IsUndefined(val) {
			return val, nil
		}
		p, ok := val.Export().(*goja.Promise)
		if !ok || p == nil {
			return val, nil
		}
		switch p.State() {
		case goja.PromiseStateFulfilled:
			val = p.Result()
		case goja.PromiseStateRejected:
			if applicationErr := e.applicationErrorFromThrown(p.Result(), e.invocation); applicationErr != nil {
				return nil, applicationErr
			}
			return nil, fmt.Errorf("function rejected: %s", rejectionDetails(p.Result()))
		default:
			<-ctx.Done()
			return nil, ctx.Err()
		}
	}
}

func rejectionDetails(value goja.Value) string {
	if obj, ok := value.(*goja.Object); ok {
		if stack := obj.Get("stack"); stack != nil && !goja.IsUndefined(stack) && !goja.IsNull(stack) {
			if text := stack.String(); text != "" {
				return text
			}
		}
	}
	return value.String()
}

func (e *entry) toHTTPResponse(v goja.Value, invocation *Invocation) (*deploy.HTTPResponseEnvelope, error) {
	resolved, err := e.resolveValue(v, invocation.Ctx)
	if err != nil {
		return nil, err
	}
	if resolved == nil || goja.IsNull(resolved) || goja.IsUndefined(resolved) {
		return nil, fmt.Errorf("httpAction handler must return a Response object")
	}
	bodyObj, ok := resolved.(*goja.Object)
	if !ok || goja.IsUndefined(bodyObj.Get("status")) {
		return nil, fmt.Errorf("httpAction handler must return a Response object")
	}

	status := int(bodyObj.Get("status").ToInteger())
	if status < 100 || status > 599 {
		return nil, fmt.Errorf("invalid HTTP response status: %d", status)
	}

	headersVal := bodyObj.Get("headers")
	headersObj, ok := headersVal.(*goja.Object)
	if !ok {
		return nil, fmt.Errorf("httpAction response has invalid headers")
	}
	exportHeaders, ok := goja.AssertFunction(headersObj.Get("_export"))
	if !ok {
		return nil, fmt.Errorf("httpAction response has invalid headers")
	}
	exportedHeaders, err := exportHeaders(headersObj)
	if err != nil {
		return nil, fmt.Errorf("httpAction response has invalid headers: %w", err)
	}
	exportedObj, ok := exportedHeaders.(*goja.Object)
	if !ok {
		return nil, fmt.Errorf("httpAction response has invalid headers")
	}
	headers := make(map[string][]string)
	for _, k := range exportedObj.Keys() {
		canonical := http.CanonicalHeaderKey(k)
		if canonical == "" {
			continue
		}
		val := exportedObj.Get(k)
		var values []string
		switch v := val.Export().(type) {
		case string:
			values = []string{v}
		case []interface{}:
			for _, item := range v {
				if s, ok := item.(string); ok {
					values = append(values, s)
				}
			}
		}
		if len(values) > 0 {
			headers[canonical] = values
		}
	}
	if err := deploy.ValidateHTTPHeaders(headers); err != nil {
		return nil, fmt.Errorf("invalid HTTP response headers: %w", err)
	}

	// Reject responses whose body has already been consumed by the handler
	// via text()/json()/arrayBuffer(). The host must not re-extract a
	// disturbed body.
	if bu := bodyObj.Get("bodyUsed"); bu != nil && bu.ToBoolean() {
		return nil, fmt.Errorf("httpAction response body has already been consumed")
	}

	bodyVal := bodyObj.Get("body")
	var body []byte
	switch v := bodyVal.Export().(type) {
	case nil:
		body = nil
	case []byte:
		body = append([]byte(nil), v...)
	case goja.ArrayBuffer:
		body = append([]byte(nil), v.Bytes()...)
	default:
		return nil, fmt.Errorf("httpAction response body is not a byte array")
	}

	if int64(len(body)) > invocation.MaxReturnBytes {
		return nil, fmt.Errorf("httpAction response body exceeds configured limit")
	}

	return &deploy.HTTPResponseEnvelope{
		Status:  status,
		Headers: headers,
		Body:    body,
	}, nil
}

// Bridge is the host bridge exposed to the JS bundle.
type Bridge struct {
	handlers             map[string]goja.Callable
	descriptors          map[string]deploy.FunctionDescriptor
	descriptorsByPath    map[string]deploy.FunctionDescriptor
	mu                   sync.RWMutex
	manifestDescriptors  []deploy.FunctionDescriptor
	migrationHandlers    map[string]migrationHandlers
	migrationDescriptors map[string]deploy.MigrationDescriptor
	manifestMigrations   []deploy.MigrationDescriptor
}

type migrationHandlers struct {
	up   goja.Callable
	down goja.Callable
}

// RegisterFunction implements globalThis.__pbvex.registerFunction(descriptor, handler).
func (b *Bridge) RegisterFunction(descriptor goja.Value, handler goja.Value) error {
	if descriptor == nil || goja.IsUndefined(descriptor) || goja.IsNull(descriptor) {
		return fmt.Errorf("descriptor must be an object")
	}
	obj, ok := descriptor.Export().(map[string]any)
	if !ok {
		return fmt.Errorf("descriptor must be an object")
	}
	fd := descriptorFromMap(obj)
	if !isValidDescriptor(fd) {
		return fmt.Errorf("descriptor has invalid fields")
	}
	if !b.matchesManifest(fd) {
		return fmt.Errorf("descriptor mismatch for %q", fd.Name)
	}
	callable, ok := goja.AssertFunction(handler)
	if !ok {
		return fmt.Errorf("handler for %q is not a function", fd.Name)
	}

	b.mu.Lock()
	if _, exists := b.handlers[fd.Name]; exists {
		b.mu.Unlock()
		return fmt.Errorf("duplicate registration for %q", fd.Name)
	}
	b.handlers[fd.Name] = callable
	b.descriptors[fd.Name] = fd
	b.descriptorsByPath[fd.Name] = fd
	b.mu.Unlock()
	return nil
}

// Verify ensures every manifest function is registered and descriptors match exactly.
func (b *Bridge) Verify(descriptors []deploy.FunctionDescriptor, migrations []deploy.MigrationDescriptor) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, expected := range descriptors {
		got, ok := b.handlers[expected.Name]
		if !ok {
			return fmt.Errorf("function %q is not registered", expected.Name)
		}
		if _, ok := b.descriptors[expected.Name]; !ok {
			return fmt.Errorf("descriptor for %q not found", expected.Name)
		}
		if !descriptorsEqual(expected, b.descriptors[expected.Name]) {
			return fmt.Errorf("descriptor mismatch for %q", expected.Name)
		}
		_ = got
	}
	if len(b.handlers) != len(descriptors) {
		return fmt.Errorf("bundle registered unexpected functions")
	}
	for _, expected := range migrations {
		if _, ok := b.migrationHandlers[expected.ID]; !ok {
			return fmt.Errorf("migration %q is not registered", expected.ID)
		}
		if got, ok := b.migrationDescriptors[expected.ID]; !ok || !migrationDescriptorsEqual(expected, got) {
			return fmt.Errorf("migration descriptor mismatch for %q", expected.ID)
		}
	}
	if len(b.migrationHandlers) != len(migrations) {
		return fmt.Errorf("bundle registered unexpected migrations")
	}
	return nil
}

// RegisterMigration implements __pbvex.registerMigration(descriptor, up, down).
func (b *Bridge) RegisterMigration(descriptor, up, down goja.Value) error {
	obj, ok := descriptor.Export().(map[string]any)
	if !ok || !exactMigrationKeys(obj) {
		return fmt.Errorf("migration descriptor must be an exact object")
	}
	var got deploy.MigrationDescriptor
	encoded, err := json.Marshal(obj)
	if err != nil || json.Unmarshal(encoded, &got) != nil {
		return fmt.Errorf("migration descriptor is invalid")
	}
	matched := false
	for _, expected := range b.manifestMigrations {
		if migrationDescriptorsEqual(expected, got) {
			matched = true
			break
		}
	}
	if !matched {
		return fmt.Errorf("migration descriptor mismatch")
	}
	upFn, upOK := goja.AssertFunction(up)
	downFn, downOK := goja.AssertFunction(down)
	if !upOK || !downOK {
		return fmt.Errorf("migration handlers must be functions")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, exists := b.migrationHandlers[got.ID]; exists {
		return fmt.Errorf("duplicate migration registration")
	}
	b.migrationHandlers[got.ID] = migrationHandlers{up: upFn, down: downFn}
	b.migrationDescriptors[got.ID] = got
	return nil
}

func migrationDescriptorsEqual(a, b deploy.MigrationDescriptor) bool {
	left, errA := deploy.CanonicalJSON(migrationDescriptorJSON(a))
	right, errB := deploy.CanonicalJSON(migrationDescriptorJSON(b))
	return errA == nil && errB == nil && left == right
}

func migrationDescriptorJSON(d deploy.MigrationDescriptor) map[string]any {
	return map[string]any{
		"id": d.ID, "table": d.Table, "mode": d.Mode, "from": d.From, "to": d.To,
		"sourceSchemaHash": d.SourceSchemaHash, "targetSchemaHash": d.TargetSchemaHash,
		"checksum": d.Checksum, "modulePath": d.ModulePath, "exportName": d.ExportName,
		"reversibility": d.Reversibility,
	}
}

func exactMigrationKeys(obj map[string]any) bool {
	if len(obj) != 11 {
		return false
	}
	for _, key := range []string{"id", "table", "mode", "from", "to", "sourceSchemaHash", "targetSchemaHash", "checksum", "modulePath", "exportName", "reversibility"} {
		if _, ok := obj[key]; !ok {
			return false
		}
	}
	return true
}

func (b *Bridge) matchesManifest(fd deploy.FunctionDescriptor) bool {
	for _, expected := range b.manifestDescriptors {
		if descriptorsEqual(expected, fd) {
			return true
		}
	}
	return false
}

func descriptorFromMap(obj map[string]any) deploy.FunctionDescriptor {
	fd := deploy.FunctionDescriptor{}
	if s, ok := obj["name"].(string); ok {
		fd.Name = s
	}
	if s, ok := obj["type"].(string); ok {
		fd.Type = deploy.FunctionType(s)
	}
	if s, ok := obj["visibility"].(string); ok {
		fd.Visibility = deploy.FunctionVisibility(s)
	}
	if s, ok := obj["modulePath"].(string); ok {
		fd.ModulePath = s
	}
	if s, ok := obj["exportName"].(string); ok {
		fd.ExportName = s
	}
	if v, ok := obj["args"]; ok {
		fd.Args = v
	}
	if v, ok := obj["returns"]; ok {
		fd.Returns = v
	}
	if v, ok := obj["route"]; ok {
		if routeMap, ok := v.(map[string]any); ok {
			fd.Route = routeFromMap(routeMap)
		}
	}
	return fd
}

func routeFromMap(obj map[string]any) *deploy.FunctionRoute {
	r := &deploy.FunctionRoute{}
	if s, ok := obj["method"].(string); ok {
		r.Method = s
	}
	if s, ok := obj["path"].(string); ok {
		r.Path = s
	}
	if s, ok := obj["pathPrefix"].(string); ok {
		r.PathPrefix = s
	}
	return r
}

func isValidDescriptor(fd deploy.FunctionDescriptor) bool {
	if fd.Name == "" || fd.ModulePath == "" || fd.ExportName == "" {
		return false
	}
	if !isFunctionType(string(fd.Type)) {
		return false
	}
	if !isFunctionVisibility(string(fd.Visibility)) {
		return false
	}
	if fd.Type == deploy.FunctionTypeHTTPAction {
		if fd.Route == nil {
			return false
		}
		if fd.Route.Method == "" || (fd.Route.Path == "" && fd.Route.PathPrefix == "") {
			return false
		}
		if fd.Route.PathPrefix != "" && !strings.HasSuffix(fd.Route.PathPrefix, "/") {
			return false
		}
	}
	return true
}

func descriptorsEqual(a, b deploy.FunctionDescriptor) bool {
	left, errA := deploy.CanonicalJSON(descriptorJSON(a))
	right, errB := deploy.CanonicalJSON(descriptorJSON(b))
	return errA == nil && errB == nil && left == right
}

func descriptorJSON(d deploy.FunctionDescriptor) map[string]any {
	o := map[string]any{"name": d.Name, "type": string(d.Type), "visibility": string(d.Visibility), "modulePath": d.ModulePath, "exportName": d.ExportName}
	if d.Args != nil {
		o["args"] = d.Args
	}
	if d.Returns != nil {
		o["returns"] = d.Returns
	}
	if d.Route != nil {
		o["route"] = routeJSON(d.Route)
	}
	return o
}

func routeJSON(r *deploy.FunctionRoute) map[string]any {
	if r == nil {
		return nil
	}
	o := map[string]any{}
	if r.Method != "" {
		o["method"] = r.Method
	}
	if r.Path != "" {
		o["path"] = r.Path
	}
	if r.PathPrefix != "" {
		o["pathPrefix"] = r.PathPrefix
	}
	return o
}

func isFunctionType(s string) bool {
	return s == "query" || s == "mutation" || s == "action" || s == "httpAction"
}

func isFunctionVisibility(s string) bool {
	return s == "public" || s == "internal"
}
