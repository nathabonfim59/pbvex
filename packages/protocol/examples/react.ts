import { useQuery, useMutation, useSubscription } from '@pbvex/react';
import { apiModule } from './generated-api.js';

// Desired React integration call sites.

export function useTask() {
  const tasks = useQuery(apiModule.tasks.listTasks);
  const updateTask = useMutation(apiModule.tasks.updateTask);
  const email = useSubscription(apiModule.email.sendEmail);
  return { tasks, updateTask, email };
}
