[pbvex](../index.md) / server

# server

## Classes

- [CronJobs](classes/CronJobs.md)
- [Expression](classes/Expression.md)
- [IndexRange](classes/IndexRange.md)

## Interfaces

- [ActionCtx](interfaces/ActionCtx.md)
- [ActionDef](interfaces/ActionDef.md)
- [AuthContext](interfaces/AuthContext.md)
- [CronJobDefinition](interfaces/CronJobDefinition.md)
- [CronJobsDefinition](interfaces/CronJobsDefinition.md)
- [DatabaseReader](interfaces/DatabaseReader.md)
- [DatabaseWriter](interfaces/DatabaseWriter.md)
- [EmailContext](interfaces/EmailContext.md)
- [EmailSendOptions](interfaces/EmailSendOptions.md)
- [FilterBuilder](interfaces/FilterBuilder.md)
- [FunctionContext](interfaces/FunctionContext.md)
- [FunctionDefinition](interfaces/FunctionDefinition.md)
- [FunctionOptions](interfaces/FunctionOptions.md)
- [GenericDocument](interfaces/GenericDocument.md)
- [Headers](interfaces/Headers.md)
- [HttpActionCtx](interfaces/HttpActionCtx.md)
- [HttpActionDef](interfaces/HttpActionDef.md)
- [HttpContext](interfaces/HttpContext.md)
- [IndexInfo](interfaces/IndexInfo.md)
- [IndexRangeBuilder](interfaces/IndexRangeBuilder.md)
- [LowerBoundIndexRangeBuilder](interfaces/LowerBoundIndexRangeBuilder.md)
- [MutationCtx](interfaces/MutationCtx.md)
- [MutationDef](interfaces/MutationDef.md)
- [OrderedQuery](interfaces/OrderedQuery.md)
- [PaginationOptions](interfaces/PaginationOptions.md)
- [PaginationResult](interfaces/PaginationResult.md)
- [Query](interfaces/Query.md)
- [QueryCtx](interfaces/QueryCtx.md)
- [QueryDef](interfaces/QueryDef.md)
- [QueryInitializer](interfaces/QueryInitializer.md)
- [Request](interfaces/Request.md)
- [Response](interfaces/Response.md)
- [SchedulerContext](interfaces/SchedulerContext.md)
- [StorageContext](interfaces/StorageContext.md)
- [StorageReader](interfaces/StorageReader.md)
- [TableInfo](interfaces/TableInfo.md)
- [TextDecoder](interfaces/TextDecoder.md)
- [TextEncoder](interfaces/TextEncoder.md)
- [UpperBoundIndexRangeBuilder](interfaces/UpperBoundIndexRangeBuilder.md)
- [URL](interfaces/URL.md)
- [URLSearchParams](interfaces/URLSearchParams.md)
- [UserIdentity](interfaces/UserIdentity.md)

## Type Aliases

- [ArgDefinition](type-aliases/ArgDefinition.md)
- [BodyInit](type-aliases/BodyInit.md)
- [DocumentByName](type-aliases/DocumentByName.md)
- [EmailTemplateVariables](type-aliases/EmailTemplateVariables.md)
- [ExpressionOrValue](type-aliases/ExpressionOrValue.md)
- [FieldPaths](type-aliases/FieldPaths.md)
- [FieldTypeFromFieldPath](type-aliases/FieldTypeFromFieldPath.md)
- [GenericDataModel](type-aliases/GenericDataModel.md)
- [HeadersInit](type-aliases/HeadersInit.md)
- [HttpAction](type-aliases/HttpAction.md)
- [Id](type-aliases/Id.md)
- [IndexNamesForTable](type-aliases/IndexNamesForTable.md)
- [InsertByName](type-aliases/InsertByName.md)
- [JobId](type-aliases/JobId.md)
- [JobID](type-aliases/JobID-1.md)
- [NamedIndex](type-aliases/NamedIndex.md)
- [NamedTableInfo](type-aliases/NamedTableInfo.md)
- [NumericValue](type-aliases/NumericValue.md)
- [PatchValue](type-aliases/PatchValue.md)
- [RequestInit](type-aliases/RequestInit.md)
- [ResponseInit](type-aliases/ResponseInit.md)
- [ReturnDefinition](type-aliases/ReturnDefinition.md)
- [SchedulableFunctionReference](type-aliases/SchedulableFunctionReference.md)
- [TableNamesInDataModel](type-aliases/TableNamesInDataModel.md)
- [Value](type-aliases/Value.md)
- [Visibility](type-aliases/Visibility.md)
- [WithoutSystemFields](type-aliases/WithoutSystemFields.md)

## Variables

- [DAY\_MS](variables/DAY_MS.md)
- [Headers](variables/Headers.md)
- [HOUR\_MS](variables/HOUR_MS.md)
- [MINUTE\_MS](variables/MINUTE_MS.md)
- [Request](variables/Request.md)
- [Response](variables/Response.md)
- [SECOND\_MS](variables/SECOND_MS.md)
- [TextDecoder](variables/TextDecoder.md)
- [TextEncoder](variables/TextEncoder.md)
- [URL](variables/URL.md)
- [URLSearchParams](variables/URLSearchParams.md)
- [WEEK\_MS](variables/WEEK_MS.md)

## Functions

- [action](functions/action.md)
- [cronJobs](functions/cronJobs.md)
- [defineComponentFns](functions/defineComponentFns.md)
- [httpAction](functions/httpAction.md)
- [internalAction](functions/internalAction.md)
- [internalMutation](functions/internalMutation.md)
- [internalQuery](functions/internalQuery.md)
- [isCronJobsDefinition](functions/isCronJobsDefinition.md)
- [isFunctionDefinition](functions/isFunctionDefinition.md)
- [mutation](functions/mutation.md)
- [normalizeArgs](functions/normalizeArgs.md)
- [normalizeReturns](functions/normalizeReturns.md)
- [query](functions/query.md)

## References

### defineApp

Re-exports [defineApp](../component/functions/defineApp.md)

***

### defineComponent

Re-exports [defineComponent](../component/functions/defineComponent.md)

***

### defineSchema

Re-exports [defineSchema](../index/functions/defineSchema.md)

***

### defineTable

Re-exports [defineTable](../index/functions/defineTable.md)

***

### FunctionArgs

Re-exports [FunctionArgs](../index/type-aliases/FunctionArgs.md)

***

### FunctionReference

Re-exports [FunctionReference](../index/type-aliases/FunctionReference.md)

***

### FunctionReturnType

Re-exports [FunctionReturnType](../index/type-aliases/FunctionReturnType.md)

***

### FunctionRoute

Re-exports [FunctionRoute](../index/type-aliases/FunctionRoute.md)

***

### FunctionType

Re-exports [FunctionType](../index/type-aliases/FunctionType.md)

***

### FunctionVisibility

Re-exports [FunctionVisibility](../index/type-aliases/FunctionVisibility.md)

***

### GenericId

Re-exports [GenericId](../values/type-aliases/GenericId.md)

***

### index

Re-exports [index](../index/functions/index.md)

***

### IndexDefinition

Re-exports [IndexDefinition](../index/interfaces/IndexDefinition.md)

***

### isSchemaDefinition

Re-exports [isSchemaDefinition](../index/functions/isSchemaDefinition.md)

***

### isTableDefinition

Re-exports [isTableDefinition](../index/functions/isTableDefinition.md)

***

### isValidator

Re-exports [isValidator](../values/functions/isValidator.md)

***

### mount

Re-exports [mount](../component/functions/mount.md)

***

### OptionalRestArgs

Re-exports [OptionalRestArgs](../index/type-aliases/OptionalRestArgs.md)

***

### SchemaDefinition

Re-exports [SchemaDefinition](../index/interfaces/SchemaDefinition.md)

***

### StorageId

Re-exports [StorageId](../index/type-aliases/StorageId.md)

***

### TableDefinition

Re-exports [TableDefinition](../index/interfaces/TableDefinition.md)
