package pbvex

import (
	"context"
	"testing"
	"time"

	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/pocketbase/pocketbase/core"
)

const schedulerCancelBundle = `
__pbvex.registerFunction({name:"scheduleVictim",type:"mutation",visibility:"public",modulePath:"pbvex/jobs.ts",exportName:"scheduleVictim"},async function(ctx){
  return await ctx.scheduler.runAfter(3600000,{_path:"victim",_type:"action",_visibility:"internal"},{});
});
__pbvex.registerFunction({name:"cancel",type:"action",visibility:"public",modulePath:"pbvex/jobs.ts",exportName:"cancel"},async function(ctx,args){
  await ctx.scheduler.cancel(args.id);
  return null;
});
__pbvex.registerFunction({name:"scheduleCanceler",type:"mutation",visibility:"public",modulePath:"pbvex/jobs.ts",exportName:"scheduleCanceler"},async function(ctx,args){
  return await ctx.scheduler.runAfter(0,{_path:"cancelScheduled",_type:"action",_visibility:"internal"},{id:args.id});
});
__pbvex.registerFunction({name:"cancelScheduled",type:"action",visibility:"internal",modulePath:"pbvex/jobs.ts",exportName:"cancelScheduled"},async function(ctx,args){
  await ctx.scheduler.cancel(args.id);
  return null;
});
__pbvex.registerFunction({name:"victim",type:"action",visibility:"internal",modulePath:"pbvex/jobs.ts",exportName:"victim"},async function(){return null;});
`

const deploymentCronBundle = `
__pbvex.registerFunction({name:"hello",type:"mutation",visibility:"public",modulePath:"hello",exportName:"default"},async function(){return "hello";});
`

func schedulerCancelUploadRequest() map[string]any {
	req := testUploadRequest("scheduler_cancel", schedulerCancelBundle, "scheduleVictim")
	req["manifest"].(map[string]any)["functions"] = []any{
		map[string]any{"name": "scheduleVictim", "type": "mutation", "visibility": "public", "modulePath": "pbvex/jobs.ts", "exportName": "scheduleVictim"},
		map[string]any{"name": "cancel", "type": "action", "visibility": "public", "modulePath": "pbvex/jobs.ts", "exportName": "cancel"},
		map[string]any{"name": "scheduleCanceler", "type": "mutation", "visibility": "public", "modulePath": "pbvex/jobs.ts", "exportName": "scheduleCanceler"},
		map[string]any{"name": "cancelScheduled", "type": "action", "visibility": "internal", "modulePath": "pbvex/jobs.ts", "exportName": "cancelScheduled"},
		map[string]any{"name": "victim", "type": "action", "visibility": "internal", "modulePath": "pbvex/jobs.ts", "exportName": "victim"},
	}
	return req
}

func waitForDurableJobStatus(t *testing.T, app core.App, id, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		record, err := app.FindRecordById(schema.CollectionJobs, id)
		if err == nil && record.GetString(schema.FieldStatus) == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	record, err := app.FindRecordById(schema.CollectionJobs, id)
	if err != nil {
		t.Fatalf("load job %q: %v", id, err)
	}
	t.Fatalf("job %q status %q, want %q", id, record.GetString(schema.FieldStatus), want)
}

func TestDeploymentCronLifecycleEnqueuesDurableJob(t *testing.T) {
	app, service := newTestApp(t)
	request := testUploadRequest("cron_one", deploymentCronBundle)
	request["manifest"].(map[string]any)["functions"].([]any)[0].(map[string]any)["type"] = "mutation"
	request["manifest"].(map[string]any)["cronJobs"] = []any{map[string]any{
		"name": "hourly-hello", "schedule": "@hourly", "functionName": "hello", "args": map[string]any{},
	}}
	response, err := service.Upload(request)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if _, err := service.Activate(response.DeploymentID, true); err != nil {
		t.Fatalf("activate: %v", err)
	}

	var cronFound bool
	for _, job := range app.Cron().Jobs() {
		if job.Id() == "pbvex:hourly-hello" {
			cronFound = true
			job.Run()
		}
	}
	if !cronFound {
		t.Fatal("activated deployment cron was not registered with PocketBase")
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var jobs []*core.Record
		if err := app.RecordQuery(schema.CollectionJobs).All(&jobs); err == nil {
			for _, job := range jobs {
				if job.GetString(schema.FieldStatus) == "completed" {
					goto completed
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("PocketBase cron tick did not produce a completed durable PBVex job")

completed:
	second := testUploadRequest("cron_two", deploymentCronBundle)
	second["manifest"].(map[string]any)["functions"].([]any)[0].(map[string]any)["type"] = "mutation"
	second["manifest"].(map[string]any)["cronJobs"] = []any{map[string]any{
		"name": "daily-hello", "schedule": "@daily", "functionName": "hello", "args": map[string]any{},
	}}
	secondResponse, err := service.Upload(second)
	if err != nil {
		t.Fatalf("second upload: %v", err)
	}
	if _, err := service.Activate(secondResponse.DeploymentID, true); err != nil {
		t.Fatalf("second activate: %v", err)
	}
	var oldFound, newFound bool
	for _, job := range app.Cron().Jobs() {
		oldFound = oldFound || job.Id() == "pbvex:hourly-hello"
		newFound = newFound || job.Id() == "pbvex:daily-hello"
	}
	if oldFound || !newFound {
		t.Fatalf("cron activation reconciliation: old=%v new=%v", oldFound, newFound)
	}
}

func TestActionSchedulerCancelUsesInvocationApp(t *testing.T) {
	app, service := newTestApp(t)
	resp, err := service.Upload(schedulerCancelUploadRequest())
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate: %v", err)
	}

	job, err := service.Call(context.Background(), "scheduleVictim", map[string]any{})
	if err != nil {
		t.Fatalf("schedule victim: %v", err)
	}
	jobID, ok := job.(string)
	if !ok || jobID == "" {
		t.Fatalf("job id %#v", job)
	}
	if _, err := service.Call(context.Background(), "cancel", map[string]any{"id": jobID}); err != nil {
		t.Fatalf("cancel from action: %v", err)
	}
	waitForDurableJobStatus(t, app, jobID, "canceled")
}

func TestScheduledActionSchedulerCancelUsesInvocationApp(t *testing.T) {
	app, service := newTestApp(t)
	resp, err := service.Upload(schedulerCancelUploadRequest())
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if _, err := service.Activate(resp.DeploymentID, true); err != nil {
		t.Fatalf("activate: %v", err)
	}

	victim, err := service.Call(context.Background(), "scheduleVictim", map[string]any{})
	if err != nil {
		t.Fatalf("schedule victim: %v", err)
	}
	victimID, ok := victim.(string)
	if !ok || victimID == "" {
		t.Fatalf("victim job id %#v", victim)
	}
	canceler, err := service.Call(context.Background(), "scheduleCanceler", map[string]any{"id": victimID})
	if err != nil {
		t.Fatalf("schedule canceler: %v", err)
	}
	cancelerID, ok := canceler.(string)
	if !ok || cancelerID == "" {
		t.Fatalf("canceler job id %#v", canceler)
	}

	waitForDurableJobStatus(t, app, cancelerID, "completed")
	waitForDurableJobStatus(t, app, victimID, "canceled")
}
