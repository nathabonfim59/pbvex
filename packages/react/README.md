# @pbvex/react

React hooks for the PBVex SDK. Provides Convex-like TypeScript ergonomics over
`@pbvex/client`.

## Install

```bash
npm install @pbvex/react @pbvex/client react react-dom
```

## Usage

```tsx
import { PBVexProvider, useQuery, useMutation, useAction } from '@pbvex/react';
import { PBVexClient } from '@pbvex/client';

const client = new PBVexClient('http://localhost:8090');

function App() {
  return (
    <PBVexProvider client={client}>
      <TaskList />
    </PBVexProvider>
  );
}

function TaskList() {
  const tasks = useQuery(api.tasks.list, { completed: false });
  const updateTask = useMutation(api.tasks.update);
  const doAction = useAction(api.tasks.notify);

  if (tasks === undefined) return <div>Loading...</div>;

  return (
    <ul>
      {tasks.map((task) => (
        <li key={task.id}>
          {task.text}
          <button onClick={() => updateTask({ id: task.id, done: true })}>
            Done
          </button>
          <button onClick={() => doAction({ taskId: task.id })}>Notify</button>
        </li>
      ))}
    </ul>
  );
}
```

## Conditional queries

Pass `"skip"` as the second argument to `useQuery` to avoid fetching.

```tsx
const profile = useQuery(api.users.get, userId ? { userId } : 'skip');
```

## Error handling

`useQuery` throws query errors so they can be caught by a React error boundary.
For explicit access to the loading state and error, use `useQueryResult`.

```tsx
const { data, error, isLoading } = useQueryResult(api.tasks.list, { completed: false });
```

## Backwards compatibility

`useSubscription` is provided as an alias for `useQuery`.

## License

MIT
