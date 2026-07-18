package deploy

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

type migrationBudget struct {
	rows  int
	bytes int64
}

type migrationBudgetContextKey struct{}
type migratedTablesContextKey struct{}

type tableMigrationPlan struct {
	table string
	steps []MigrationDescriptor
}

func withMigrationMaterialization(ctx context.Context, budget *migrationBudget, tables map[string]bool) context.Context {
	ctx = context.WithValue(ctx, migrationBudgetContextKey{}, budget)
	return context.WithValue(ctx, migratedTablesContextKey{}, tables)
}

func documentDescriptor(rawSchema any, table string) (any, bool) {
	o, _ := rawSchema.(map[string]any)
	for _, raw := range listJSON(o["tables"]) {
		t, _ := raw.(map[string]any)
		if t["tableName"] == table {
			fields, ok := t["fields"].(map[string]any)
			return map[string]any{"type": "object", "shape": fields}, ok
		}
	}
	return nil, false
}

func tableFields(rawSchema any, table string) (map[string]any, bool) {
	descriptor, ok := documentDescriptor(rawSchema, table)
	if !ok {
		return nil, false
	}
	fields, ok := descriptor.(map[string]any)["shape"].(map[string]any)
	return fields, ok
}

func planMigrations(source, target DeploymentManifest) ([]tableMigrationPlan, error) {
	tables := schemaTableNames(target.Schema)
	plans := make([]tableMigrationPlan, 0)
	for table := range tables {
		from, sourceExists := documentDescriptor(source.Schema, table)
		to, _ := documentDescriptor(target.Schema, table)
		goal, err := CanonicalHash(to)
		if err != nil {
			return nil, fmt.Errorf("migration target schema is invalid for table %q", table)
		}
		candidates := make([]MigrationDescriptor, 0)
		bySource := map[string]MigrationDescriptor{}
		for _, candidate := range target.Migrations {
			if candidate.Table != table {
				continue
			}
			if _, exists := bySource[candidate.SourceSchemaHash]; exists {
				return nil, fmt.Errorf("ambiguous migration chain for table %q", table)
			}
			bySource[candidate.SourceSchemaHash] = candidate
			candidates = append(candidates, candidate)
		}
		for _, candidate := range candidates {
			hash := candidate.TargetSchemaHash
			seenHashes := map[string]bool{}
			for hash != goal {
				if seenHashes[hash] {
					return nil, fmt.Errorf("cyclic migration chain for table %q", table)
				}
				seenHashes[hash] = true
				next, ok := bySource[hash]
				if !ok {
					return nil, fmt.Errorf("migration chain for table %q does not reach target schema", table)
				}
				hash = next.TargetSchemaHash
			}
		}
		if !sourceExists {
			continue
		}
		current, err := CanonicalHash(from)
		if err != nil {
			return nil, fmt.Errorf("migration planning failed")
		}
		if current == goal {
			continue
		}
		plan := tableMigrationPlan{table: table}
		seen := map[string]bool{}
		for current != goal {
			var next *MigrationDescriptor
			if candidate, ok := bySource[current]; ok {
				copy := candidate
				next = &copy
			}
			if next == nil {
				if len(candidates) > 0 {
					return nil, fmt.Errorf("migration chain from active schema for table %q does not reach target schema", table)
				}
				break
			}
			if seen[next.ID] {
				return nil, fmt.Errorf("cyclic migration chain")
			}
			seen[next.ID] = true
			plan.steps = append(plan.steps, *next)
			current = next.TargetSchemaHash
		}
		if len(plan.steps) > 0 {
			if current != goal {
				return nil, fmt.Errorf("migration chain does not reach target schema")
			}
			plans = append(plans, plan)
		}
	}
	sort.Slice(plans, func(i, j int) bool {
		left, right := plans[i].table, plans[j].table
		if len(plans[i].steps) > 0 {
			left = plans[i].steps[0].ID
		}
		if len(plans[j].steps) > 0 {
			right = plans[j].steps[0].ID
		}
		return left < right
	})
	return plans, nil
}

func schemaMigrationWork(source, target DeploymentManifest, migrationPlans []tableMigrationPlan) ([]tableMigrationPlan, map[string]bool, error) {
	work := append([]tableMigrationPlan(nil), migrationPlans...)
	skipMaterialization := map[string]bool{}
	planned := map[string]bool{}
	for _, plan := range migrationPlans {
		planned[plan.table] = true
		skipMaterialization[plan.table] = true
	}
	for table := range schemaTableNames(target.Schema) {
		if planned[table] {
			continue
		}
		sourceTable, sourceExists := rawTableDescriptor(source.Schema, table)
		targetTable, _ := rawTableDescriptor(target.Schema, table)
		if !sourceExists {
			continue
		}
		before, beforeErr := CanonicalHash(sourceTable)
		after, afterErr := CanonicalHash(targetTable)
		if beforeErr != nil || afterErr != nil {
			return nil, nil, fmt.Errorf("schema migration planning failed for table %q", table)
		}
		if before == after {
			skipMaterialization[table] = true
			continue
		}
		work = append(work, tableMigrationPlan{table: table})
	}
	return work, skipMaterialization, nil
}

func rawTableDescriptor(rawSchema any, table string) (map[string]any, bool) {
	o, _ := rawSchema.(map[string]any)
	for _, raw := range listJSON(o["tables"]) {
		if descriptor, ok := raw.(map[string]any); ok && descriptor["tableName"] == table {
			return descriptor, true
		}
	}
	return nil, false
}

func preflightMigrationPlans(ctx context.Context, app core.App, plans []tableMigrationPlan) (int, int64, error) {
	rows := 0
	var sourceBytes int64
	for _, plan := range plans {
		if _, err := app.FindCollectionByNameOrId(plan.table); errors.Is(err, sql.ErrNoRows) {
			continue
		} else if err != nil {
			return 0, 0, fmt.Errorf("migration preflight failed")
		}
		count, err := backingRecordCount(ctx, app, plan.table)
		if err != nil || count > int64(maxSchemaMigrationRows-rows) {
			return 0, 0, fmt.Errorf("schema migration exceeds limit")
		}
		rows += int(count)
		records := []*core.Record{}
		if err := app.RecordQuery(plan.table).WithContext(ctx).All(&records); err != nil {
			return 0, 0, fmt.Errorf("migration preflight failed")
		}
		for _, record := range records {
			encoded, err := CanonicalJSON(recordData(record))
			if err != nil || int64(len(encoded)) > maxSchemaMigrationBytes-sourceBytes {
				return 0, 0, fmt.Errorf("schema migration exceeds limit")
			}
			sourceBytes += int64(len(encoded))
		}
	}
	return rows, sourceBytes, nil
}

func recordData(record *core.Record) map[string]any {
	out := map[string]any{}
	encoded, err := json.Marshal(record.Get("_pbvex_data"))
	if err == nil {
		_ = json.Unmarshal(encoded, &out)
	}
	return out
}

func migrationIDEncoder(ctx context.Context, app core.App) (func(string, string) (string, error), error) {
	state := &core.Record{}
	if err := app.RecordQuery(schema.CollectionSchemaState).WithContext(ctx).AndWhere(dbx.HashExp{schema.CollectionSchemaState + "." + schema.FieldKey: schema.StateKeyActive}).Limit(1).One(state); err != nil {
		return nil, fmt.Errorf("schema state unavailable")
	}
	key, err := base64.RawURLEncoding.DecodeString(state.GetString(schema.FieldIDSecret))
	if err != nil || len(key) < 32 || state.GetInt(schema.FieldIDKeyID) < 1 {
		return nil, fmt.Errorf("schema state unavailable")
	}
	keyID := state.GetInt(schema.FieldIDKeyID)
	return func(table, raw string) (string, error) {
		payload, err := json.Marshal(struct {
			V int    `json:"v"`
			K int    `json:"k"`
			N string `json:"n"`
			T string `json:"t"`
			R string `json:"r"`
		}{2, keyID, RootNamespace, table, raw})
		if err != nil {
			return "", err
		}
		return "pbv2." + strconv.Itoa(keyID) + "." + base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(schema.OpaqueIDVersionMAC(key, keyID, payload)), nil
	}, nil
}

func (s *Service) applyMigrationPlans(ctx context.Context, app core.App, deploymentID, direction string, target DeploymentManifest, plans []tableMigrationPlan, activationTime types.DateTime, budget *migrationBudget) error {
	invoker, ok := s.invoker.(interface {
		InvokeMigration(context.Context, string, string, string, any, int64) (any, error)
	})
	if !ok && len(plans) > 0 {
		return fmt.Errorf("migration runtime is not configured")
	}
	encodeID, err := migrationIDEncoder(ctx, app)
	if err != nil && len(plans) > 0 {
		return err
	}
	for _, plan := range plans {
		steps := append([]MigrationDescriptor(nil), plan.steps...)
		if direction == "down" {
			for left, right := 0, len(steps)-1; left < right; left, right = left+1, right-1 {
				steps[left], steps[right] = steps[right], steps[left]
			}
		}
		fields, _ := tableFields(target.Schema, plan.table)
		idCheck, err := activeDocumentIDChecker(ctx, app, RootNamespace)
		if err != nil {
			return err
		}
		rows := []*core.Record{}
		if err := app.RecordQuery(plan.table).WithContext(ctx).OrderBy("id ASC").All(&rows); err != nil {
			return fmt.Errorf("migration read failed")
		}
		for _, row := range rows {
			budget.rows++
			if budget.rows > maxSchemaMigrationRows {
				return fmt.Errorf("schema migration exceeds limit")
			}
			data := recordData(row)
			before, err := CanonicalJSON(data)
			if err != nil || chargeMigrationBytes(&budget.bytes, before) != nil {
				return fmt.Errorf("schema migration exceeds limit")
			}
			for _, step := range steps {
				from, to := step.From, step.To
				if direction == "down" {
					from, to = step.To, step.From
				}
				normalized, err := schema.NormalizeValue(from, data, idCheck)
				if err != nil {
					return fmt.Errorf("migration source document invalid")
				}
				input, _ := normalized.(map[string]any)
				normalizedInput, err := CanonicalJSON(input)
				if err != nil || chargeMigrationBytes(&budget.bytes, normalizedInput) != nil {
					return fmt.Errorf("schema migration exceeds limit")
				}
				id, err := encodeID(plan.table, row.Id)
				if err != nil {
					return fmt.Errorf("migration id unavailable")
				}
				document := make(map[string]any, len(input)+2)
				for key, value := range input {
					document[key] = value
				}
				document["_id"] = id
				document["_creationTime"] = float64(row.GetDateTime("created").Time().UnixMilli())
				output, err := invoker.InvokeMigration(ctx, deploymentID, step.ID, direction, document, activationTime.Time().UnixMilli())
				if err != nil {
					return fmt.Errorf("migration %q failed", step.ID)
				}
				object, ok := output.(map[string]any)
				_, hasID := object["_id"]
				_, hasCreationTime := object["_creationTime"]
				if !ok || hasID || hasCreationTime {
					return fmt.Errorf("migration output invalid")
				}
				rawOutput, err := CanonicalJSON(object)
				if err != nil || chargeMigrationBytes(&budget.bytes, rawOutput) != nil {
					return fmt.Errorf("schema migration exceeds limit")
				}
				next, err := schema.NormalizeValue(to, object, idCheck)
				if err != nil {
					return fmt.Errorf("migration target document invalid")
				}
				data, ok = next.(map[string]any)
				if !ok {
					return fmt.Errorf("migration output invalid")
				}
				normalizedOutput, err := CanonicalJSON(data)
				if err != nil || chargeMigrationBytes(&budget.bytes, normalizedOutput) != nil {
					return fmt.Errorf("schema migration exceeds limit")
				}
			}
			data, err = schema.NormalizeDocument(fields, data, false, true, idCheck)
			if err != nil {
				return fmt.Errorf("target schema document invalid")
			}
			projection, err := schema.OrderDataWithID(fields, data, idCheck)
			if err != nil {
				return fmt.Errorf("schema index unsupported")
			}
			encoded, _ := CanonicalJSON(data)
			projectionJSON, _ := CanonicalJSON(projection)
			if chargeMigrationBytes(&budget.bytes, encoded) != nil || chargeMigrationBytes(&budget.bytes, projectionJSON) != nil {
				return fmt.Errorf("schema migration exceeds limit")
			}
			row.Set("_pbvex_data", data)
			row.Set(schema.DocumentOrderField, projection)
			if err := app.SaveWithContext(ctx, row); err != nil {
				return fmt.Errorf("migration write failed")
			}
		}
		for _, step := range steps {
			if err := s.recordMigrationHistory(ctx, app, deploymentID, direction, step, activationTime); err != nil {
				return err
			}
		}
	}
	return nil
}

func preflightMigrationHistory(ctx context.Context, app core.App, migrations []MigrationDescriptor) error {
	for _, descriptor := range migrations {
		records := []*core.Record{}
		if err := app.RecordQuery(schema.CollectionMigrationHistory).WithContext(ctx).AndWhere(dbx.HashExp{schema.CollectionMigrationHistory + "." + schema.FieldMigrationID: descriptor.ID}).All(&records); err != nil {
			return fmt.Errorf("migration history unavailable")
		}
		for _, record := range records {
			if record.GetString(schema.FieldChecksum) != descriptor.Checksum || record.GetString(schema.FieldSourceHash) != descriptor.SourceSchemaHash || record.GetString(schema.FieldTargetHash) != descriptor.TargetSchemaHash {
				return fmt.Errorf("migration id was reused with different content")
			}
		}
	}
	return nil
}

func (s *Service) recordMigrationHistory(ctx context.Context, app core.App, deploymentID, direction string, descriptor MigrationDescriptor, appliedAt types.DateTime) error {
	existing := []*core.Record{}
	if err := app.RecordQuery(schema.CollectionMigrationHistory).WithContext(ctx).AndWhere(dbx.HashExp{schema.CollectionMigrationHistory + "." + schema.FieldMigrationID: descriptor.ID}).All(&existing); err != nil {
		return fmt.Errorf("migration history unavailable")
	}
	for _, record := range existing {
		if record.GetString(schema.FieldChecksum) != descriptor.Checksum || record.GetString(schema.FieldSourceHash) != descriptor.SourceSchemaHash || record.GetString(schema.FieldTargetHash) != descriptor.TargetSchemaHash {
			return fmt.Errorf("migration id was reused with different content")
		}
	}
	collection, err := app.FindCollectionByNameOrId(schema.CollectionMigrationHistory)
	if err != nil {
		return fmt.Errorf("migration history unavailable")
	}
	record := core.NewRecord(collection)
	record.Set(schema.FieldMigrationID, descriptor.ID)
	record.Set(schema.FieldChecksum, descriptor.Checksum)
	record.Set(schema.FieldSourceHash, descriptor.SourceSchemaHash)
	record.Set(schema.FieldTargetHash, descriptor.TargetSchemaHash)
	record.Set(schema.FieldDeploymentID, deploymentID)
	record.Set(schema.FieldDirection, direction)
	record.Set(schema.FieldAppliedAt, appliedAt)
	if err := app.SaveWithContext(ctx, record); err != nil {
		return fmt.Errorf("migration history write failed")
	}
	return nil
}

func reverseMigrationPlans(plans []tableMigrationPlan) []tableMigrationPlan {
	out := append([]tableMigrationPlan(nil), plans...)
	for left, right := 0, len(out)-1; left < right; left, right = left+1, right-1 {
		out[left], out[right] = out[right], out[left]
	}
	return out
}

func migrationWarning(rows int, estimatedBytes int64) *MigrationWarning {
	rowPercent := rows * 100 / maxSchemaMigrationRows
	bytePercent := int(estimatedBytes * 100 / maxSchemaMigrationBytes)
	utilization := rowPercent
	if bytePercent > utilization {
		utilization = bytePercent
	}
	if utilization < 80 {
		return nil
	}
	return &MigrationWarning{Code: "transactional_migration_utilization", Rows: rows, RowLimit: maxSchemaMigrationRows, EstimatedBytes: estimatedBytes, ByteLimit: maxSchemaMigrationBytes, UtilizationPercent: utilization}
}
