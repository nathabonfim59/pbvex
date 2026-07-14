import { Client, Query, query } from '@pbvex/sdk-core';

// Desired query definition and call site.

export const listTasks = query('listTasks', async () => {
  return [{ _id: 't1', title: 'Schema example', done: false }];
});

const client = new Client();

async function callSite(): Promise<void> {
  const tasks = await client.query('listTasks');
  const direct = await listTasks.run();
  const viaQuery = await new Query('listTasks').run();
  console.log(tasks, direct, viaQuery);
}

export { callSite };
