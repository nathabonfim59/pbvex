import { api } from '@pbvex/client';
import { query, mutation, action } from '@pbvex/client';

// Desired generated API call site.

const listTasks = query('listTasks', async () => []);
const updateTask = mutation('updateTask', async (_id: string, _done: boolean) => ({}));
const sendEmail = action('sendEmail', async (_to: string, _subject: string) => ({}));

export const apiModule = api({
  tasks: { listTasks, updateTask },
  email: { sendEmail },
});

// Generated API access: api.tasks.listTasks.run(...)
async function callSite(): Promise<void> {
  const tasks = await apiModule.tasks.listTasks.run();
  await apiModule.tasks.updateTask.run('t1', true);
  await apiModule.email.sendEmail.run('user@example.com', 'Hello');
  console.log(tasks);
}

export { callSite };
