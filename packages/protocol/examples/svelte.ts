import { useQuery, useMutation, useSubscription } from '@pbvex/svelte';
import { apiModule } from './generated-api.js';

// Desired Svelte 5 integration call sites. Invoke this during component initialization.

export function createTaskState() {
  const tasks = useQuery(apiModule.tasks.listTasks);
  const updateTask = useMutation(apiModule.tasks.updateTask);
  const compatibilityQuery = useSubscription(apiModule.tasks.listTasks);
  return { tasks, updateTask, compatibilityQuery };
}
