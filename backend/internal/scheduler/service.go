package scheduler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/nathabonfim59/pbvex/backend/internal/runtime"
	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

const (
	JobTypeScheduled = "scheduled"

	JobStatusPending   = "pending"
	JobStatusRunning   = "running"
	JobStatusCanceled  = "canceled"
	JobStatusFailed    = "failed"
	JobStatusCompleted = "completed"

	maxDelay        = 5 * 365 * 24 * time.Hour
	maxScheduledAt  = 100 * 365 * 24 * time.Hour // effectively far-future cap
	errorFieldLimit = 5000
)

func dateTime(t time.Time) types.DateTime {
	d, _ := types.ParseDateTime(t)
	return d
}

// Config controls the scheduler service.
type Config struct {
	PollInterval         time.Duration
	MaxConcurrency       int
	LeaseDuration        time.Duration
	RenewInterval        time.Duration
	MaxExecutionDuration time.Duration
	ClaimBatch           int
	RetryInitialDelay    time.Duration
	RetryMaxDelay        time.Duration
	MaxAttempts          int
	CleanupInterval      time.Duration
	CleanupBatch         int
	JobHistoryRetention  time.Duration
	Jitter               func(time.Duration) time.Duration
	Clock                Clock
}

// DefaultConfig returns sensible defaults for the scheduler.
func DefaultConfig() Config {
	lease := 2 * time.Minute
	return Config{
		PollInterval:         2 * time.Second,
		MaxConcurrency:       5,
		LeaseDuration:        lease,
		RenewInterval:        lease / 2,
		MaxExecutionDuration: lease,
		ClaimBatch:           10,
		RetryInitialDelay:    1 * time.Second,
		RetryMaxDelay:        1 * time.Minute,
		MaxAttempts:          5,
		CleanupInterval:      1 * time.Hour,
		CleanupBatch:         1000,
		JobHistoryRetention:  7 * 24 * time.Hour,
		Jitter:               defaultRandJitter,
		Clock:                NewRealClock(),
	}
}

func defaultRandJitter(base time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(base)))
}

// JobExecutor runs a scheduled function and returns its wire-encoded result.
// Resolve exposes the deployment snapshot that the scheduler pins per job.
// Pin atomically increments/decrements the per-deployment job reference counter.
type JobExecutor interface {
	InvokeDeploymentSnapshot(ctx context.Context, deploymentID, bundleHash, functionName string, args any) (any, error)
	Resolve(ctx context.Context, deploymentID string) (deploy.DeploymentManifest, string, string, error)
	Pin(ctx context.Context, deploymentID string, delta int) error
}

// JobStatus is the operator-observable view of a job.
// It never includes the raw payload or args.
type JobStatus struct {
	ID             string    `json:"id"`
	DeploymentID   string    `json:"deploymentId"`
	Type           string    `json:"type"`
	Status         string    `json:"status"`
	ScheduledAt    time.Time `json:"scheduledAt"`
	Started        time.Time `json:"started,omitempty"`
	Finished       time.Time `json:"finished,omitempty"`
	Attempts       int       `json:"attempts"`
	Lease          string    `json:"-"`
	LeaseExpiresAt time.Time `json:"leaseExpiresAt,omitempty"`
	Result         any       `json:"result,omitempty"`
	Error          string    `json:"error,omitempty"`
	Metadata       any       `json:"metadata,omitempty"`
}

// ListResult is returned by List.
type ListResult struct {
	Total      int         `json:"total"`
	Items      []JobStatus `json:"items"`
	Limit      int         `json:"limit"`
	NextCursor string      `json:"nextCursor,omitempty"`
	HasMore    bool        `json:"hasMore"`
}

// Service is the durable scheduler implementation.
type Service struct {
	app      core.App
	executor JobExecutor
	config   Config
	clock    Clock
	worker   *Worker

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	startOnce sync.Once
	stopOnce  sync.Once
}

// NewService creates a new scheduler service. Call Start to begin processing.
func NewService(app core.App, executor JobExecutor, config Config) *Service {
	if config.PollInterval <= 0 {
		config.PollInterval = DefaultConfig().PollInterval
	}
	if config.MaxConcurrency <= 0 {
		config.MaxConcurrency = DefaultConfig().MaxConcurrency
	}
	if config.LeaseDuration <= 0 {
		config.LeaseDuration = DefaultConfig().LeaseDuration
	}
	if config.RenewInterval <= 0 {
		config.RenewInterval = DefaultConfig().RenewInterval
	}
	// Ensure the heartbeat renews before the lease expires. A renew interval
	// >= lease duration would let the lease lapse before renewal, causing
	// unnecessary thefts. Use half the lease as a safe margin. If the lease
	// is so small that half is zero, disable the heartbeat (0) rather than
	// violating the strict invariant RenewInterval < LeaseDuration.
	if config.RenewInterval >= config.LeaseDuration {
		config.RenewInterval = config.LeaseDuration / 2
		if config.RenewInterval <= 0 {
			config.RenewInterval = 0
		}
	}
	if config.MaxExecutionDuration <= 0 {
		config.MaxExecutionDuration = DefaultConfig().MaxExecutionDuration
	}
	if config.ClaimBatch <= 0 {
		config.ClaimBatch = DefaultConfig().ClaimBatch
	}
	if config.RetryInitialDelay <= 0 {
		config.RetryInitialDelay = DefaultConfig().RetryInitialDelay
	}
	if config.RetryMaxDelay <= 0 {
		config.RetryMaxDelay = DefaultConfig().RetryMaxDelay
	}
	if config.MaxAttempts <= 0 {
		config.MaxAttempts = DefaultConfig().MaxAttempts
	}
	if config.CleanupInterval <= 0 {
		config.CleanupInterval = DefaultConfig().CleanupInterval
	}
	if config.CleanupBatch <= 0 {
		config.CleanupBatch = DefaultConfig().CleanupBatch
	}
	if config.JobHistoryRetention <= 0 {
		config.JobHistoryRetention = DefaultConfig().JobHistoryRetention
	}
	if config.Jitter == nil {
		config.Jitter = DefaultConfig().Jitter
	}
	if config.Clock == nil {
		config.Clock = NewRealClock()
	}

	s := &Service{
		app:      app,
		executor: executor,
		config:   config,
		clock:    config.Clock,
	}
	s.worker = NewWorker(s)
	return s
}

// Start begins the worker and cleanup loops.
func (s *Service) Start(ctx context.Context) error {
	var startErr error
	s.startOnce.Do(func() {
		s.ctx, s.cancel = context.WithCancel(ctx)
		if err := s.worker.Start(s.ctx); err != nil {
			startErr = err
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.cleanupLoop(s.ctx)
		}()
	})
	return startErr
}

// Stop gracefully shuts down the worker and cleanup loops.
func (s *Service) Stop() {
	s.stopOnce.Do(func() {
		if s.worker != nil {
			s.worker.Stop()
		}
		if s.cancel != nil {
			s.cancel()
		}
		s.wg.Wait()
	})
}

// RunAfter schedules a function to run after delayMs.
func (s *Service) RunAfter(ctx context.Context, delayMs int64, deploymentID, functionName string, args any) (string, error) {
	if delayMs < 0 {
		return "", fmt.Errorf("delayMs must be non-negative")
	}
	if delayMs > int64(maxDelay/time.Millisecond) {
		return "", fmt.Errorf("delayMs exceeds maximum allowed delay")
	}
	scheduledAt := s.clock.Now().Add(time.Duration(delayMs) * time.Millisecond)
	return s.scheduleJob(ctx, scheduledAt, deploymentID, functionName, args)
}

// RunAt schedules a function to run at a wall-clock time.
func (s *Service) RunAt(ctx context.Context, epochMs int64, deploymentID, functionName string, args any) (string, error) {
	scheduledAt := time.UnixMilli(epochMs)
	if err := validateScheduledAt(s.clock, scheduledAt); err != nil {
		return "", err
	}
	return s.scheduleJob(ctx, scheduledAt, deploymentID, functionName, args)
}

// Cancel marks a pending or running job as canceled. On lease-CAS miss
// (the lease changed between read and update due to a concurrent reclaim),
// it reloads and retries boundedly while the job is still cancelable.
func (s *Service) Cancel(ctx context.Context, id string) error {
	app, ok := schema.AppFromContext(ctx)
	if !ok {
		return fmt.Errorf("missing app in context")
	}

	const maxRetries = 3
	for attempt := 0; attempt <= maxRetries; attempt++ {
		record, err := app.FindRecordById(schema.CollectionJobs, id)
		if err != nil {
			return ErrJobNotFound
		}
		if requester, ok := runtime.ScheduleNamespacesFromContext(ctx); ok && requester.Owner != "" {
			owner, _, payloadErr := payloadNamespaces(record)
			if payloadErr != nil || owner != requester.Owner {
				return ErrJobNotCancelable
			}
		}

		status := record.GetString(schema.FieldStatus)
		if status != JobStatusPending && status != JobStatusRunning {
			return ErrJobNotCancelable
		}

		leaseToken := record.GetString(schema.FieldLease)
		now := s.clock.Now()
		res, err := app.DB().Update(
			schema.CollectionJobs,
			dbx.Params{
				schema.FieldStatus:         JobStatusCanceled,
				schema.FieldLease:          "",
				schema.FieldLeaseExpiresAt: types.DateTime{},
				schema.FieldFinished:       dateTime(now),
				schema.FieldUpdated:        dateTime(now),
			},
			dbx.NewExp("id = {:id} AND lease = {:lease} AND (status = 'pending' OR status = 'running')", dbx.Params{"id": id, "lease": leaseToken}),
		).Execute()
		if err != nil {
			return err
		}

		affected, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if affected > 0 {
			if status == JobStatusRunning {
				if app.IsTransactional() {
					app.TxInfo().OnComplete(func(txErr error) error {
						if txErr == nil {
							s.worker.Cancel(id, leaseToken)
						}
						return nil
					})
				} else {
					s.worker.Cancel(id, leaseToken)
				}
			}
			return nil
		}
		// CAS miss: the lease changed between read and update. Reload and
		// retry while the job is still pending or running.
	}
	return ErrJobNotCancelable
}

// Retry requeues a terminal job. It rejects the request if the deployment snapshot
// the job is pinned to has been trimmed or replaced so that a worker cannot be
// asked to invoke a missing snapshot.
func (s *Service) Retry(ctx context.Context, id string) error {
	app, ok := schema.AppFromContext(ctx)
	if !ok {
		app = s.app
	}
	record, err := app.FindRecordById(schema.CollectionJobs, id)
	if err != nil {
		return ErrJobNotFound
	}

	status := record.GetString(schema.FieldStatus)
	if status != JobStatusCompleted && status != JobStatusFailed && status != JobStatusCanceled {
		return ErrJobNotRetryable
	}

	deploymentID, bundleHash, functionName, _, err := parsePayload(record)
	if err != nil {
		return ErrJobNotRetryable
	}

	return app.RunInTransaction(func(txApp core.App) error {
		txCtx := schema.WithApp(ctx, txApp)

		manifest, resolvedHash, _, err := s.executor.Resolve(txCtx, deploymentID)
		if err != nil {
			if errors.Is(err, deploy.ErrDeploymentNotFound) {
				return ErrDeploymentSnapshotNotFound
			}
			return err
		}
		if resolvedHash != bundleHash {
			return ErrDeploymentSnapshotNotFound
		}

		if _, _, err := findFunction(manifest, functionName); err != nil {
			return ErrJobNotRetryable
		}

		now := s.clock.Now()
		res, err := txApp.DB().Update(
			schema.CollectionJobs,
			dbx.Params{
				schema.FieldStatus:         JobStatusPending,
				schema.FieldScheduledAt:    dateTime(now),
				schema.FieldAttempts:       0,
				schema.FieldStarted:        types.DateTime{},
				schema.FieldFinished:       types.DateTime{},
				schema.FieldError:          "",
				schema.FieldResult:         "null",
				schema.FieldMetadata:       "null",
				schema.FieldLease:          "",
				schema.FieldLeaseExpiresAt: types.DateTime{},
				schema.FieldUpdated:        dateTime(now),
			},
			dbx.NewExp("id = {:id} AND status IN ('completed','failed','canceled')", dbx.Params{"id": id}),
		).Execute()
		if err != nil {
			return err
		}
		affected, err := res.RowsAffected()
		if err != nil || affected == 0 {
			return ErrJobNotRetryable
		}

		s.maybeWake(txCtx)
		return nil
	})
}

// Get returns a single job by id.
func (s *Service) Get(ctx context.Context, id string) (*JobStatus, error) {
	app, ok := schema.AppFromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("missing app in context")
	}
	record, err := app.FindRecordById(schema.CollectionJobs, id)
	if err != nil {
		return nil, ErrJobNotFound
	}
	status := jobStatusFromRecord(record)
	return &status, nil
}

// List returns jobs with optional status filter and keyset pagination.
// status is one of the JobStatus* constants or empty for all.
func (s *Service) List(ctx context.Context, status string, limit int, cursor string) (*ListResult, error) {
	app, ok := schema.AppFromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("missing app in context")
	}
	if status != "" {
		if !isValidStatus(status) {
			return nil, ErrJobInvalidStatus
		}
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	var c *listCursor
	if cursor != "" {
		decoded, err := base64.URLEncoding.DecodeString(cursor)
		if err != nil {
			return nil, ErrJobInvalidStatus
		}
		c = &listCursor{}
		if err := json.Unmarshal(decoded, c); err != nil {
			return nil, ErrJobInvalidStatus
		}
	}

	var total int64
	var err error
	if status != "" {
		total, err = app.CountRecords(schema.CollectionJobs, dbx.NewExp("status = {:status}", dbx.Params{"status": status}))
	} else {
		total, err = app.CountRecords(schema.CollectionJobs)
	}
	if err != nil {
		return nil, err
	}

	query := app.RecordQuery(schema.CollectionJobs)
	if status != "" {
		query = query.AndWhere(dbx.NewExp("status = {:status}", dbx.Params{"status": status}))
	}
	if c != nil {
		query = query.AndWhere(dbx.NewExp(
			"(scheduledAt < {:sa} OR (scheduledAt = {:sa} AND id < {:id}))",
			dbx.Params{"sa": dateTime(c.ScheduledAt), "id": c.ID},
		))
	}

	records := []*core.Record{}
	err = query.OrderBy("scheduledAt DESC", "id DESC").Limit(int64(limit + 1)).All(&records)
	if err != nil {
		return nil, err
	}

	hasMore := len(records) > limit
	if hasMore {
		records = records[:limit]
	}

	items := make([]JobStatus, len(records))
	for i, record := range records {
		items[i] = jobStatusFromRecord(record)
	}

	result := &ListResult{
		Total:   int(total),
		Items:   items,
		Limit:   limit,
		HasMore: hasMore,
	}
	if hasMore && len(items) > 0 {
		last := items[len(items)-1]
		next := listCursor{ScheduledAt: last.ScheduledAt, ID: last.ID}
		raw, _ := json.Marshal(next)
		result.NextCursor = base64.URLEncoding.EncodeToString(raw)
	}

	return result, nil
}

func isValidStatus(status string) bool {
	switch status {
	case JobStatusPending, JobStatusRunning, JobStatusCompleted, JobStatusFailed, JobStatusCanceled:
		return true
	}
	return false
}

type listCursor struct {
	ScheduledAt time.Time `json:"scheduledAt"`
	ID          string    `json:"id"`
}

func (s *Service) scheduleJob(ctx context.Context, scheduledAt time.Time, deploymentID, functionName string, args any) (string, error) {
	app, ok := schema.AppFromContext(ctx)
	if !ok {
		app = s.app
	}
	if app == nil {
		return "", fmt.Errorf("missing app in context")
	}

	var id string
	err := app.RunInTransaction(func(txApp core.App) error {
		txCtx := schema.WithApp(ctx, txApp)

		manifest, bundleHash, _, err := s.executor.Resolve(txCtx, deploymentID)
		if err != nil {
			return fmt.Errorf("invalid deployment: %w", err)
		}

		desc, cfg, err := findFunction(manifest, functionName)
		if err != nil {
			return fmt.Errorf("invalid function: %w", err)
		}

		targetNamespace := deploy.RootNamespace
		if namespace, ok := deploy.NamespaceForModule(manifest, desc.ModulePath); ok {
			targetNamespace = namespace.ID
		}
		scheduleNamespaces, hasNamespaces := runtime.ScheduleNamespacesFromContext(ctx)
		ownerNamespace := deploy.RootNamespace
		if hasNamespaces {
			ownerNamespace = scheduleNamespaces.Owner
			if scheduleNamespaces.Target != "" && scheduleNamespaces.Target != targetNamespace {
				return fmt.Errorf("invalid scheduled target namespace")
			}
		}
		payload, err := buildPayload(deploymentID, desc.Name, bundleHash, args, ownerNamespace, targetNamespace, cfg.MaxFunctionArgsBytes)
		if err != nil {
			return err
		}

		// Pin the deployment snapshot before inserting the job so a concurrent
		// trim cannot delete the snapshot between Resolve and Save.
		if err := s.executor.Pin(txCtx, deploymentID, +1); err != nil {
			if errors.Is(err, deploy.ErrDeploymentNotFound) {
				return fmt.Errorf("invalid deployment: %w", err)
			}
			return err
		}

		col, err := txApp.FindCollectionByNameOrId(schema.CollectionJobs)
		if err != nil {
			return err
		}

		record := core.NewRecord(col)
		record.Set(schema.FieldDeploymentID, deploymentID)
		record.Set(schema.FieldType, JobTypeScheduled)
		record.Set(schema.FieldStatus, JobStatusPending)
		record.Set(schema.FieldPayload, payload)
		record.Set(schema.FieldScheduledAt, dateTime(scheduledAt))
		record.Set(schema.FieldAttempts, 0)
		now := s.clock.Now()
		record.Set(schema.FieldCreated, dateTime(now))
		record.Set(schema.FieldUpdated, dateTime(now))

		if err := txApp.SaveWithContext(txCtx, record); err != nil {
			return err
		}

		id = record.Id
		s.maybeWake(txCtx)
		return nil
	})
	return id, err
}

func (s *Service) maybeWake(ctx context.Context) {
	app, ok := schema.AppFromContext(ctx)
	if !ok || !app.IsTransactional() {
		s.worker.Wake()
		return
	}
	app.TxInfo().OnComplete(func(txErr error) error {
		if txErr == nil {
			s.worker.Wake()
		}
		return nil
	})
}

func (s *Service) cleanupLoop(ctx context.Context) {
	interval := s.config.CleanupInterval + s.config.Jitter(s.config.CleanupInterval)
	ticker := s.clock.NewTicker(interval)
	defer func() { ticker.Stop() }()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.Chan():
			if err := s.cleanupOld(ctx); err != nil {
				s.app.Logger().Warn("job cleanup failed", "error", err)
			}
			interval = s.config.CleanupInterval + s.config.Jitter(s.config.CleanupInterval)
			ticker.Stop()
			ticker = s.clock.NewTicker(interval)
		}
	}
}

func (s *Service) cleanupOld(ctx context.Context) error {
	if s.config.JobHistoryRetention <= 0 {
		return nil
	}
	cutoff := s.clock.Now().Add(-s.config.JobHistoryRetention)
	return s.app.RunInTransaction(func(txApp core.App) error {
		records := []*core.Record{}
		err := txApp.RecordQuery(schema.CollectionJobs).
			AndWhere(dbx.NewExp("status IN ('completed','failed','canceled') AND finished < {:cutoff}", dbx.Params{"cutoff": dateTime(cutoff)})).
			OrderBy("finished ASC").
			Limit(int64(s.config.CleanupBatch)).
			All(&records)
		if err != nil {
			return err
		}
		txCtx := schema.WithApp(s.internalCtx(), txApp)
		for _, rec := range records {
			deploymentID := rec.GetString(schema.FieldDeploymentID)
			if err := s.executor.Pin(txCtx, deploymentID, -1); err != nil {
				if errors.Is(err, deploy.ErrDeploymentNotFound) {
					// Deployment already trimmed; safe to delete the stale job.
				} else if errors.Is(err, deploy.ErrPinUnderflow) {
					// pinCount already 0 — don't delete the job so the
					// reconciliation record survives and migration can
					// correct the counter. Skip and continue.
					continue
				} else {
					// Unexpected error: abort so the job record is preserved
					// and the transaction rolls back.
					return fmt.Errorf("pin decrement for job %s: %w", rec.Id, err)
				}
			}
			if err := txApp.DeleteWithContext(txCtx, rec); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Service) internalCtx() context.Context {
	return context.WithValue(context.Background(), schema.InternalContextKey, true)
}

func (s *Service) retryBackoff(attempts int) time.Duration {
	base := s.config.RetryInitialDelay
	for i := 0; i < attempts-1 && base < s.config.RetryMaxDelay; i++ {
		base *= 2
		if base > s.config.RetryMaxDelay {
			base = s.config.RetryMaxDelay
		}
	}
	jitter := s.config.Jitter(base)
	delay := base + jitter
	if delay > s.config.RetryMaxDelay {
		delay = s.config.RetryMaxDelay
	}
	return delay
}

func validateScheduledAt(clock Clock, t time.Time) error {
	min := clock.Now().Add(-time.Minute)
	max := clock.Now().Add(maxScheduledAt)
	if t.Before(min) {
		return fmt.Errorf("scheduledAt must be in the future")
	}
	if t.After(max) {
		return fmt.Errorf("scheduledAt exceeds maximum allowed schedule time")
	}
	return nil
}

func findFunction(manifest deploy.DeploymentManifest, functionName string) (deploy.FunctionDescriptor, deploy.DeploymentConfig, error) {
	cfg := deploy.NormalizeConfig(manifest.Config)
	for _, d := range manifest.Functions {
		if d.Name == functionName {
			if d.Type != deploy.FunctionTypeMutation && d.Type != deploy.FunctionTypeAction {
				return deploy.FunctionDescriptor{}, cfg, fmt.Errorf("function %q is not schedulable", functionName)
			}
			if d.Visibility != deploy.FunctionVisibilityPublic && d.Visibility != deploy.FunctionVisibilityInternal {
				return deploy.FunctionDescriptor{}, cfg, fmt.Errorf("function %q is not schedulable", functionName)
			}
			return d, cfg, nil
		}
	}
	return deploy.FunctionDescriptor{}, cfg, fmt.Errorf("function %q not found", functionName)
}

func buildPayload(deploymentID, functionName, bundleHash string, args any, ownerNamespace, targetNamespace string, maxArgsBytes int64) (map[string]any, error) {
	canonical, err := deploy.CanonicalJSON(args)
	if err != nil {
		return nil, fmt.Errorf("invalid scheduled function args: %w", err)
	}
	if int64(len(canonical)) > maxArgsBytes {
		return nil, fmt.Errorf("scheduled function args exceed configured size limit")
	}
	return map[string]any{
		"deploymentId":    deploymentID,
		"functionName":    functionName,
		"bundleHash":      bundleHash,
		"args":            args,
		"ownerNamespace":  ownerNamespace,
		"targetNamespace": targetNamespace,
	}, nil
}

func payloadNamespaces(record *core.Record) (owner, target string, err error) {
	var payload map[string]any
	if err = record.UnmarshalJSONField(schema.FieldPayload, &payload); err != nil {
		return "", "", err
	}
	owner, _ = payload["ownerNamespace"].(string)
	target, _ = payload["targetNamespace"].(string)
	return owner, target, nil
}

func parsePayload(record *core.Record) (deploymentID, bundleHash, functionName string, args any, err error) {
	deploymentID, bundleHash, functionName, args, _, _, err = parsePayloadWithNamespaces(record)
	return
}

func parsePayloadWithNamespaces(record *core.Record) (deploymentID, bundleHash, functionName string, args any, ownerNamespace, targetNamespace string, err error) {
	var payload map[string]any
	if err = record.UnmarshalJSONField(schema.FieldPayload, &payload); err != nil {
		return "", "", "", nil, "", "", fmt.Errorf("failed to decode job payload: %w", err)
	}
	deploymentID, _ = payload["deploymentId"].(string)
	bundleHash, _ = payload["bundleHash"].(string)
	functionName, _ = payload["functionName"].(string)
	args = payload["args"]
	ownerNamespace, _ = payload["ownerNamespace"].(string)
	targetNamespace, _ = payload["targetNamespace"].(string)
	if deploymentID == "" || functionName == "" || ownerNamespace == "" || targetNamespace == "" {
		return "", "", "", nil, "", "", fmt.Errorf("job payload missing deploymentId, functionName, ownerNamespace, or targetNamespace")
	}
	return deploymentID, bundleHash, functionName, args, ownerNamespace, targetNamespace, nil
}

func jobStatusFromRecord(record *core.Record) JobStatus {
	var result any
	_ = record.UnmarshalJSONField(schema.FieldResult, &result)
	var metadata any
	_ = record.UnmarshalJSONField(schema.FieldMetadata, &metadata)
	return JobStatus{
		ID:             record.Id,
		DeploymentID:   record.GetString(schema.FieldDeploymentID),
		Type:           record.GetString(schema.FieldType),
		Status:         record.GetString(schema.FieldStatus),
		ScheduledAt:    record.GetDateTime(schema.FieldScheduledAt).Time(),
		Started:        record.GetDateTime(schema.FieldStarted).Time(),
		Finished:       record.GetDateTime(schema.FieldFinished).Time(),
		Attempts:       record.GetInt(schema.FieldAttempts),
		Lease:          record.GetString(schema.FieldLease),
		LeaseExpiresAt: record.GetDateTime(schema.FieldLeaseExpiresAt).Time(),
		Result:         result,
		Error:          record.GetString(schema.FieldError),
		Metadata:       metadata,
	}
}

func truncateError(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if len(s) > errorFieldLimit {
		return s[:errorFieldLimit]
	}
	return s
}
