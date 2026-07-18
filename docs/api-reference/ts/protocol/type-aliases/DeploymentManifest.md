[@pbvex/protocol](../index.md) / DeploymentManifest

# Type Alias: DeploymentManifest

> **DeploymentManifest** = `Readonly`\<\{ `components?`: [`ComponentGraph`](ComponentGraph.md); `config?`: `Partial`\<[`DeploymentConfig`](DeploymentConfig.md)\>; `cronJobs?`: [`CronJobDescriptor`](CronJobDescriptor.md)[]; `deploymentId`: `string`; `emailTemplates?`: [`EmailTemplateManifest`](EmailTemplateManifest.md); `functions?`: [`FunctionDescriptor`](FunctionDescriptor.md)[]; `migrations?`: [`MigrationDescriptor`](MigrationDescriptor.md)[]; `protocolVersion`: [`DeploymentProtocolVersion`](DeploymentProtocolVersion.md); `schema?`: [`SchemaDescriptor`](SchemaDescriptor.md); \}\>
