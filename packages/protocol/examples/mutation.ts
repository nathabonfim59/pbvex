import { Client, Mutation, mutation } from '@pbvex/client';

// Desired mutation definition and call site.

export const updateTask = mutation('updateTask', async (id: string, done: boolean) => {
  return { _id: id, done };
});

const client = new Client();

async function callSite(): Promise<void> {
  const result = await client.mutation('updateTask', 't1', true);
  const direct = await updateTask.run('t1', true);
  const viaMutation = await new Mutation('updateTask').run('t1', true);
  console.log(result, direct, viaMutation);
}

export { callSite };
