import type {
  DeploymentManifest,
  DeploymentUploadRequest,
  FunctionDescriptor,
  SchemaDescriptor,
  ComponentGraph,
  ModuleSource,
} from '@pbvex/protocol';
import { DEFAULT_CONFIG as ProtocolDefaultConfig, canonicalHash, hashSha256Bytes, hashSha256 } from '@pbvex/protocol';
import type { FunctionDefinition } from '../runtime/server.js';
import type { CronJobsDefinition } from '../runtime/server.js';
import type { SchemaDefinition } from '../schema/schema.js';
import { deriveFunctionName } from './functionName.js';
import type { ComponentDefinitionWithKind, AppDefinition } from '../runtime/component.js';
import { buildComponentGraph } from '../runtime/component.js';

export interface ModuleEntry {
  path: string;
  code: string;
  imports: ModuleImport[];
  exports: string[];
}

export interface ModuleImport {
  specifier: string;
  resolvedSpecifier?: string;
  names: string[];
  defaultName?: string;
  namespaceName?: string;
  kind: 'value' | 'type';
  position: SourcePosition;
}

export interface SourcePosition {
  file: string;
  line: number;
  column: number;
}

export interface DeploymentArtifact extends Omit<DeploymentUploadRequest, 'modules'> {
  project?: string;
  target: string;
  modules: ModuleEntry[];
}

export interface BuildMetadata {
  project?: string;
  target: string;
  modules: ModuleEntry[];
  diagnostics: string[];
}

function codeUnitCompare(a: string, b: string): number {
  return a < b ? -1 : a > b ? 1 : 0;
}

export function toUploadRequest(artifact: DeploymentArtifact): DeploymentUploadRequest {
	const { project: _project, target: _target, modules, ...upload } = artifact;
	const sources: ModuleSource[] = modules
		.map((module) => ({ path: module.path, bytes: Buffer.from(module.code, 'utf8').toString('base64') }))
		.sort((a, b) => codeUnitCompare(a.path, b.path));
	return { ...upload, modules: upload.manifest.components ? sources : undefined };
}

export async function createArtifact(
  project: string | undefined,
  target: string,
  functions: FunctionDefinition<any, any, any>[],
  components: ComponentDefinitionWithKind[],
  app: AppDefinition | undefined,
  schema: SchemaDefinition | undefined,
  modules: ModuleEntry[],
  bundle: string,
  componentSourceFunctionPaths?: string[],
  componentSourceModules?: ModuleEntry[],
  emailTemplates: import('@pbvex/protocol').EmailTemplate[] = [],
  cronJobs?: CronJobsDefinition,
): Promise<DeploymentArtifact> {
  const encoder = new TextEncoder();
  const bundleBytes = encoder.encode(bundle);
  const bundleBase64 = Buffer.from(bundleBytes).toString('base64');
  const sha256 = await hashSha256Bytes(bundleBytes.buffer as ArrayBuffer);
  const emailTemplateManifest = emailTemplates.length ? {
    entries: emailTemplates,
    sha256: await canonicalHash({ bundleSha256: sha256, entries: emailTemplates }),
  } : undefined;
  const deploymentIdentity = `${project ?? ''}|${target}|${sha256}` + (emailTemplateManifest ? `|${emailTemplateManifest.sha256}` : '');
  const deploymentId = 'dep_' + (await hashSha256(deploymentIdentity));

  const functionManifest: FunctionDescriptor[] = functions
    .filter((fn) => fn.modulePath && fn.exportName)
    .sort((a, b) => codeUnitCompare(`${a.modulePath}:${a.exportName}`, `${b.modulePath}:${b.exportName}`))
    .map((fn) => {
      const args = (fn.args as any).toJSON ? (fn.args as any).toJSON() : undefined;
      const returns = (fn.returns as any).toJSON ? (fn.returns as any).toJSON() : undefined;
      return {
        name: deriveFunctionName(fn.modulePath, fn.exportName),
        type: fn.type,
        visibility: fn.visibility,
        modulePath: fn.modulePath,
        exportName: fn.exportName,
        args,
        returns,
        route: fn.route,
      } as FunctionDescriptor;
    });
  const cronJobManifest = cronJobs?.jobs
    .map((job) => ({ ...job }))
    .sort((a, b) => codeUnitCompare(a.name, b.name));
  if (cronJobManifest) {
    const functionsByName = new Map(functionManifest.map((fn) => [fn.name, fn]));
    for (const job of cronJobManifest) {
      const target = functionsByName.get(job.functionName);
      if (!target || (target.type !== 'mutation' && target.type !== 'action')) {
        throw new Error(`Cron job ${job.name} target is not a deployed mutation or action`);
      }
    }
  }

  const schemaDescriptor: SchemaDescriptor | undefined = schema
    ? (schema.toJSON() as unknown as SchemaDescriptor)
    : undefined;
	const componentGraph: ComponentGraph | undefined = await buildComponentGraph(
		components,
		app,
		componentSourceFunctionPaths ?? functionManifest.map((fn) => fn.modulePath),
		componentSourceModules ?? modules,
		sha256,
	);

  const manifest: DeploymentManifest = {
    protocolVersion: 'v1',
    deploymentId,
    functions: functionManifest,
		components: componentGraph,
    config: ProtocolDefaultConfig,
    schema: schemaDescriptor,
    emailTemplates: emailTemplateManifest,
    cronJobs: cronJobManifest?.length ? cronJobManifest : undefined,
  };

  return {
    project,
    target,
    manifest,
    bundle: bundleBase64,
    sha256,
    size: bundleBytes.byteLength,
    modules: modules.sort((a, b) => codeUnitCompare(a.path, b.path)),
  };
}

export function artifactToJson(artifact: DeploymentArtifact): string {
  return JSON.stringify(toUploadRequest(artifact), null, 2);
}

export function buildMetadataToJson(metadata: BuildMetadata): string {
  return JSON.stringify(metadata, null, 2);
}
