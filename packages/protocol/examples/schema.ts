import { id, type Id, type DeploymentManifest, validateManifest } from '@pbvex/protocol';

// Desired schema call sites for table IDs and manifest validation.

type Task = {
  _id: Id<'tasks'>;
  title: string;
  done: boolean;
};

const taskId: Id<'tasks'> = id('tasks', 'task_01');
const manifest: DeploymentManifest = {
  protocolVersion: 'v1',
  deploymentId: 'deployment_schema',
  functions: [
    {
      name: 'getTask',
      type: 'query',
      visibility: 'public',
      modulePath: 'convex/tasks.ts',
      exportName: 'default',
      args: {
        type: 'object',
        fields: {
          id: { type: 'id', tableName: 'tasks' },
        },
      },
      returns: {
        type: 'object',
        fields: {
          _id: { type: 'id', tableName: 'tasks' },
          title: { type: 'string' },
          done: { type: 'boolean' },
        },
      },
    },
    {
      name: 'updateTask',
      type: 'mutation',
      visibility: 'public',
      modulePath: 'convex/tasks.ts',
      exportName: 'updateTask',
      args: {
        type: 'object',
        fields: {
          id: { type: 'id', tableName: 'tasks' },
          title: { type: 'string' },
          done: { type: 'boolean' },
        },
      },
      returns: { type: 'id', tableName: 'tasks' },
    },
    {
      name: 'healthCheck',
      type: 'httpAction',
      visibility: 'public',
      modulePath: 'convex/http.ts',
      exportName: 'healthCheck',
      route: { method: 'GET', path: 'health' },
    },
  ],
};

validateManifest(manifest);

export type { Task };
export { taskId, manifest };
