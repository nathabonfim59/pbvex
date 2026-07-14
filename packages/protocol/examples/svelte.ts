import { useQuery, useMutation, useSubscription } from '@pbvex/sdk-svelte';
import { apiModule } from './generated-api.js';

// Desired Svelte integration call sites.

export function createTaskStore() {
  const tasks = useQuery(apiModule.tasks.listTasks);
  const updateTask = useMutation(apiModule.tasks.updateTask);
  const email = useSubscription(apiModule.email.sendEmail);
  return { tasks, updateTask, email };
}
