package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/google/uuid"
	"github.com/nathabonfim59/pbvex/backend/internal/deploy"
	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

// Worker is the bounded background processor for scheduled jobs.
// It claims rows with a CAS on the observed status, lease token, and expiry so
// stale workers can never overwrite a new owner.
//
// Once a job is claimed, a per-attempt lease token is assigned. A heartbeat
// goroutine renews the lease while the job runs. If the lease is lost (another
// worker steals the row), the running invocation is canceled. Completion,
// failure, retry, and release all require the same lease token.
//
// This gives at-least-once delivery: a job may be run by multiple workers if
// a previous lease expires, but a stale worker cannot record a result after a
// new owner has taken over.
type Worker struct {
	service *Service

	id    string
	clock Clock
	sem   chan struct{}

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	wake       chan struct{}
	cancellers sync.Map

	startOnce sync.Once
	stopOnce  sync.Once
}

// cancellation is the value stored in cancellers. It is keyed by job id and
// lease token so that a reclaimed attempt cannot overwrite or be overwritten by
// a previous attempt.
type cancellation struct {
	cancel context.CancelFunc
}

// NewWorker creates a worker bound to the given service.
func NewWorker(service *Service) *Worker {
	return &Worker{
		service: service,
		id:      uuid.NewString(),
		clock:   service.config.Clock,
		sem:     make(chan struct{}, service.config.MaxConcurrency),
		wake:    make(chan struct{}, 1),
	}
}

// Start begins the polling loop.
func (w *Worker) Start(ctx context.Context) error {
	var startErr error
	w.startOnce.Do(func() {
		w.ctx, w.cancel = context.WithCancel(ctx)
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			ticker := w.clock.NewTicker(w.service.config.PollInterval)
			defer ticker.Stop()
			for {
				select {
				case <-w.ctx.Done():
					return
				case <-w.wake:
					w.tick()
				case <-ticker.Chan():
					w.tick()
				}
			}
		}()
	})
	return startErr
}

// Stop cancels the worker and waits for all goroutines to finish.
func (w *Worker) Stop() {
	w.stopOnce.Do(func() {
		if w.cancel != nil {
			w.cancel()
		}
		w.wg.Wait()
	})
}

// Wake triggers an immediate poll attempt.
func (w *Worker) Wake() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Cancel requests that a running attempt stop.
// The leaseToken scopes the cancel to the current attempt, so a stale
// cancellation cannot accidentally remove the cancel func of a reclaimed attempt.
func (w *Worker) Cancel(id, leaseToken string) bool {
	key := id + "/" + leaseToken
	val, ok := w.cancellers.Load(key)
	if !ok {
		return false
	}
	canceller, ok := val.(*cancellation)
	if !ok {
		return false
	}
	canceller.cancel()
	w.cancellers.CompareAndDelete(key, canceller)
	return true
}

func (w *Worker) tick() {
	if w.ctx.Err() != nil {
		return
	}
	w.claim()
}

func (w *Worker) claim() {
	if w.ctx.Err() != nil {
		return
	}
	if w.service.app.DB() == nil {
		return
	}

	now := w.clock.Now()
	records := []*core.Record{}
	err := w.service.app.RecordQuery(schema.CollectionJobs).
		AndWhere(dbx.NewExp("((status = {:pending} AND scheduledAt <= {:now}) OR (status = {:running} AND leaseExpiresAt <= {:now}))", dbx.Params{
			"pending": JobStatusPending,
			"running": JobStatusRunning,
			"now":     dateTime(now),
		})).
		OrderBy("scheduledAt ASC", "created ASC").
		Limit(int64(w.service.config.ClaimBatch)).
		All(&records)
	if err != nil {
		w.service.app.Logger().Warn("job claim query failed", "error", err)
		return
	}

	for _, record := range records {
		if w.ctx.Err() != nil {
			return
		}

		// Acquire execution capacity before taking the durable claim.
		select {
		case w.sem <- struct{}{}:
		case <-w.ctx.Done():
			return
		}

		claimed, err := w.claimOne(record, now)
		if err != nil {
			<-w.sem
			w.service.app.Logger().Warn("job claim failed", "jobId", record.Id, "error", err)
			continue
		}
		if !claimed {
			<-w.sem
			continue
		}

		w.wg.Add(1)
		go w.runJob(record)
	}
}

func (w *Worker) claimOne(record *core.Record, now time.Time) (bool, error) {
	if w.service.app.DB() == nil {
		return false, nil
	}
	status := record.GetString(schema.FieldStatus)
	leaseDuration := w.service.config.LeaseDuration
	nowDateTime := dateTime(now)
	leaseExpiresAt := dateTime(now.Add(leaseDuration))
	newLease := uuid.NewString()

	var where dbx.Expression
	switch status {
	case JobStatusPending:
		where = dbx.NewExp(
			"id = {:id} AND status = {:pending} AND lease = '' AND leaseExpiresAt = '' AND scheduledAt <= {:now}",
			dbx.Params{
				"id":      record.Id,
				"pending": JobStatusPending,
				"now":     nowDateTime,
			},
		)
	case JobStatusRunning:
		where = dbx.NewExp(
			"id = {:id} AND status = {:running} AND lease = {:lease} AND leaseExpiresAt = {:leaseExpiresAt} AND leaseExpiresAt <= {:now}",
			dbx.Params{
				"id":             record.Id,
				"running":        JobStatusRunning,
				"lease":          record.GetString(schema.FieldLease),
				"leaseExpiresAt": dateTime(record.GetDateTime(schema.FieldLeaseExpiresAt).Time()),
				"now":            nowDateTime,
			},
		)
	default:
		return false, nil
	}

	res, err := w.service.app.DB().Update(
		schema.CollectionJobs,
		dbx.Params{
			schema.FieldStatus:         JobStatusRunning,
			schema.FieldLease:          newLease,
			schema.FieldLeaseExpiresAt: leaseExpiresAt,
			schema.FieldAttempts:       dbx.NewExp("COALESCE(attempts, 0) + 1"),
			schema.FieldStarted:        dbx.NewExp("CASE WHEN started = '' THEN {:now} ELSE started END", dbx.Params{"now": nowDateTime}),
			schema.FieldUpdated:        nowDateTime,
		},
		where,
	).Execute()
	if err != nil {
		return false, err
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected == 0 {
		return false, nil
	}

	record.Set(schema.FieldStatus, JobStatusRunning)
	record.Set(schema.FieldLease, newLease)
	record.Set(schema.FieldLeaseExpiresAt, leaseExpiresAt)
	record.Set(schema.FieldAttempts, record.GetInt(schema.FieldAttempts)+1)
	if record.GetDateTime(schema.FieldStarted).IsZero() {
		record.Set(schema.FieldStarted, nowDateTime)
	}
	record.Set(schema.FieldUpdated, nowDateTime)
	return true, nil
}

func (w *Worker) runJob(record *core.Record) {
	defer w.wg.Done()
	defer func() { <-w.sem }()

	jobCtx, cancel := context.WithCancel(w.ctx)
	leaseToken := record.GetString(schema.FieldLease)
	cancelKey := record.Id + "/" + leaseToken
	canceller := &cancellation{cancel: cancel}
	w.cancellers.Store(cancelKey, canceller)
	defer func() {
		cancel()
		w.cancellers.CompareAndDelete(cancelKey, canceller)
	}()

	defer func() {
		if r := recover(); r != nil {
			w.fail(record, fmt.Errorf("panic: %v", r))
		}
	}()

	// Close the claim-to-canceller race: if Cancel set status=canceled (or a
	// reclaim changed the lease) between claimOne and canceller registration,
	// the invocation must not start. The canceller is now registered, so any
	// subsequent Cancel will signal via the context.
	stillOurs, err := w.service.app.CountRecords(schema.CollectionJobs, dbx.NewExp(
		"id = {:id} AND status = {:running} AND lease = {:lease}",
		dbx.Params{"id": record.Id, "running": JobStatusRunning, "lease": leaseToken},
	))
	if err != nil || stillOurs == 0 {
		return
	}

	deploymentID, bundleHash, functionName, args, _, targetNamespace, err := parsePayloadWithNamespaces(record)
	if err != nil {
		w.fail(record, err)
		return
	}
	if bundleHash == "" {
		w.fail(record, fmt.Errorf("job payload missing bundle hash"))
		return
	}
	manifest, resolvedHash, _, err := w.service.executor.Resolve(jobCtx, deploymentID)
	if err != nil || resolvedHash != bundleHash {
		w.fail(record, fmt.Errorf("scheduled deployment snapshot mismatch"))
		return
	}
	descriptor, _, err := findFunction(manifest, functionName)
	if err != nil {
		w.fail(record, fmt.Errorf("scheduled function unavailable: %w", err))
		return
	}
	resolvedNamespace := deploy.RootNamespace
	if namespace, ok := deploy.NamespaceForModule(manifest, descriptor.ModulePath); ok {
		resolvedNamespace = namespace.ID
	}
	if targetNamespace != resolvedNamespace {
		w.fail(record, fmt.Errorf("scheduled target namespace mismatch"))
		return
	}

	// Heartbeat: extend the lease while the job runs.
	renewCtx, renewCancel := context.WithCancel(jobCtx)
	var renewWg sync.WaitGroup
	defer func() {
		renewCancel()
		renewWg.Wait()
	}()

	renewWg.Add(1)
	go func() {
		defer renewWg.Done()
		w.renewLoop(renewCtx, cancel, record.Id, leaseToken)
	}()

	// Bound the overall invocation time.
	var execCtx context.Context
	var execCancel context.CancelFunc
	if d := w.service.config.MaxExecutionDuration; d > 0 {
		execCtx, execCancel = context.WithTimeout(jobCtx, d)
	} else {
		execCtx, execCancel = jobCtx, func() {}
	}
	defer execCancel()

	result, err := w.service.executor.InvokeDeploymentSnapshot(execCtx, deploymentID, bundleHash, functionName, args)
	renewCancel()
	renewWg.Wait()
	if err != nil {
		w.handleError(record, err)
		return
	}

	w.complete(record, result)
}

func (w *Worker) renewLoop(ctx context.Context, cancel context.CancelFunc, jobID string, leaseToken string) {
	if w.service.config.LeaseDuration <= 0 || w.service.config.RenewInterval <= 0 {
		return
	}
	renewInterval := w.service.config.RenewInterval
	ticker := w.clock.NewTicker(renewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.Chan():
			if !w.renew(jobID, leaseToken) {
				cancel()
				return
			}
		}
	}
}

func (w *Worker) renew(jobID string, leaseToken string) bool {
	if w.service.app.DB() == nil {
		return false
	}
	now := w.clock.Now()
	newLeaseExpiresAt := dateTime(now.Add(w.service.config.LeaseDuration))
	res, err := w.service.app.DB().Update(
		schema.CollectionJobs,
		dbx.Params{
			schema.FieldLeaseExpiresAt: newLeaseExpiresAt,
			schema.FieldUpdated:        dateTime(now),
		},
		dbx.NewExp("id = {:id} AND status = {:running} AND lease = {:lease}", dbx.Params{
			"id":      jobID,
			"running": JobStatusRunning,
			"lease":   leaseToken,
		}),
	).Execute()
	if err != nil {
		w.service.app.Logger().Warn("job lease renew failed", "jobId", jobID, "error", err)
		return false
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false
	}
	if affected == 0 {
		return false
	}
	return true
}

func (w *Worker) handleError(record *core.Record, err error) {
	if errors.Is(err, context.Canceled) {
		if w.ctx.Err() != nil {
			w.release(record)
		}
		return
	}

	var gojaErr *goja.Exception
	if errors.As(err, &gojaErr) {
		w.fail(record, err)
		return
	}

	if errors.Is(err, deploy.ErrDeploymentNotFound) ||
		errors.Is(err, deploy.ErrForbidden) ||
		errors.Is(err, deploy.ErrActiveNotFound) ||
		errors.Is(err, deploy.ErrInvalidBundle) ||
		errors.Is(err, deploy.ErrInvalidManifest) ||
		errors.Is(err, deploy.ErrActivationFailed) {
		w.fail(record, err)
		return
	}

	if errors.Is(err, context.DeadlineExceeded) {
		w.retry(record)
		return
	}

	w.retry(record)
}

func (w *Worker) complete(record *core.Record, result any) {
	resultJSON, err := deploy.CanonicalJSON(result)
	if err != nil {
		w.fail(record, err)
		return
	}

	now := w.clock.Now()
	nowDateTime := dateTime(now)
	res, err := w.service.app.DB().Update(
		schema.CollectionJobs,
		dbx.Params{
			schema.FieldStatus:         JobStatusCompleted,
			schema.FieldResult:         resultJSON,
			schema.FieldMetadata:       "null",
			schema.FieldFinished:       nowDateTime,
			schema.FieldLease:          "",
			schema.FieldLeaseExpiresAt: types.DateTime{},
			schema.FieldUpdated:        nowDateTime,
		},
		dbx.NewExp("id = {:id} AND status = {:running} AND lease = {:lease}", dbx.Params{
			"id":      record.Id,
			"running": JobStatusRunning,
			"lease":   record.GetString(schema.FieldLease),
		}),
	).Execute()
	if err != nil {
		w.service.app.Logger().Warn("job complete failed", "jobId", record.Id, "error", err)
		return
	}
	affected, err := res.RowsAffected()
	if err != nil {
		w.service.app.Logger().Warn("job complete rows affected failed", "jobId", record.Id, "error", err)
		return
	}
	if affected == 0 {
		w.service.app.Logger().Warn("stale completion ignored", "jobId", record.Id)
	}
}

func (w *Worker) fail(record *core.Record, err error) {
	now := w.clock.Now()
	nowDateTime := dateTime(now)
	metadata := map[string]any{
		"errorType":    errorType(err),
		"errorMessage": truncateError(err),
	}
	metadataJSON, _ := deploy.CanonicalJSON(metadata)
	res, updateErr := w.service.app.DB().Update(
		schema.CollectionJobs,
		dbx.Params{
			schema.FieldStatus:         JobStatusFailed,
			schema.FieldError:          truncateError(err),
			schema.FieldMetadata:       metadataJSON,
			schema.FieldResult:         "null",
			schema.FieldFinished:       nowDateTime,
			schema.FieldLease:          "",
			schema.FieldLeaseExpiresAt: types.DateTime{},
			schema.FieldUpdated:        nowDateTime,
		},
		dbx.NewExp("id = {:id} AND status = {:running} AND lease = {:lease}", dbx.Params{
			"id":      record.Id,
			"running": JobStatusRunning,
			"lease":   record.GetString(schema.FieldLease),
		}),
	).Execute()
	if updateErr != nil {
		w.service.app.Logger().Warn("job fail failed", "jobId", record.Id, "error", updateErr)
		return
	}
	affected, updateErr := res.RowsAffected()
	if updateErr != nil {
		w.service.app.Logger().Warn("job fail rows affected failed", "jobId", record.Id, "error", updateErr)
		return
	}
	if affected == 0 {
		w.service.app.Logger().Warn("stale failure ignored", "jobId", record.Id)
		return
	}
	w.service.app.Logger().Warn("job failed", "jobId", record.Id, "errorType", metadata["errorType"], "error", err, "attempts", record.GetInt(schema.FieldAttempts))
}

func (w *Worker) retry(record *core.Record) {
	attempts := record.GetInt(schema.FieldAttempts)
	if attempts >= w.service.config.MaxAttempts {
		w.fail(record, errMaxAttemptsExceeded)
		return
	}

	delay := w.service.retryBackoff(attempts)
	scheduledAt := dateTime(w.clock.Now().Add(delay))
	now := dateTime(w.clock.Now())

	res, err := w.service.app.DB().Update(
		schema.CollectionJobs,
		dbx.Params{
			schema.FieldStatus:         JobStatusPending,
			schema.FieldScheduledAt:    scheduledAt,
			schema.FieldLease:          "",
			schema.FieldLeaseExpiresAt: types.DateTime{},
			schema.FieldResult:         "null",
			schema.FieldMetadata:       "null",
			schema.FieldError:          "",
			schema.FieldUpdated:        now,
		},
		dbx.NewExp("id = {:id} AND status = {:running} AND lease = {:lease}", dbx.Params{
			"id":      record.Id,
			"running": JobStatusRunning,
			"lease":   record.GetString(schema.FieldLease),
		}),
	).Execute()
	if err != nil {
		w.service.app.Logger().Warn("job retry failed", "jobId", record.Id, "error", err)
		return
	}
	affected, err := res.RowsAffected()
	if err != nil {
		w.service.app.Logger().Warn("job retry rows affected failed", "jobId", record.Id, "error", err)
		return
	}
	if affected == 0 {
		w.service.app.Logger().Warn("stale retry ignored", "jobId", record.Id)
	}
}

func (w *Worker) release(record *core.Record) {
	now := dateTime(w.clock.Now())
	res, err := w.service.app.DB().Update(
		schema.CollectionJobs,
		dbx.Params{
			schema.FieldStatus:         JobStatusPending,
			schema.FieldScheduledAt:    now,
			schema.FieldLease:          "",
			schema.FieldLeaseExpiresAt: types.DateTime{},
			schema.FieldUpdated:        now,
		},
		dbx.NewExp("id = {:id} AND status = {:running} AND lease = {:lease}", dbx.Params{
			"id":      record.Id,
			"running": JobStatusRunning,
			"lease":   record.GetString(schema.FieldLease),
		}),
	).Execute()
	if err != nil {
		w.service.app.Logger().Warn("job release failed", "jobId", record.Id, "error", err)
		return
	}
	affected, err := res.RowsAffected()
	if err != nil {
		w.service.app.Logger().Warn("job release rows affected failed", "jobId", record.Id, "error", err)
		return
	}
	if affected == 0 {
		w.service.app.Logger().Warn("stale release ignored", "jobId", record.Id)
	}
}

func errorType(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, errMaxAttemptsExceeded) {
		return "max_attempts"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	var gojaErr *goja.Exception
	if errors.As(err, &gojaErr) {
		return "goja_exception"
	}
	if errors.Is(err, deploy.ErrDeploymentNotFound) {
		return "deployment_not_found"
	}
	if errors.Is(err, deploy.ErrForbidden) {
		return "forbidden"
	}
	if errors.Is(err, deploy.ErrActiveNotFound) {
		return "active_not_found"
	}
	if errors.Is(err, deploy.ErrInvalidBundle) {
		return "invalid_bundle"
	}
	if errors.Is(err, deploy.ErrInvalidManifest) {
		return "invalid_manifest"
	}
	if errors.Is(err, deploy.ErrActivationFailed) {
		return "activation_failed"
	}
	return "internal_error"
}
