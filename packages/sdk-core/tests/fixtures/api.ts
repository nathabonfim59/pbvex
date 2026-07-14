import type { FunctionReference } from 'pbvex/server';

export const api = {
  messages: {
    list: {
      _path: 'messages.list',
      _type: 'query',
      _visibility: 'public',
    } as FunctionReference<'query', { channel: string }, string[], 'public'>,
    send: {
      _path: 'messages.send',
      _type: 'mutation',
      _visibility: 'public',
    } as FunctionReference<'mutation', { body: string }, { id: string }, 'public'>,
  },
} as const;

export const internal = {
  messages: {
    admin: {
      _path: 'messages.admin',
      _type: 'query',
      _visibility: 'internal',
    } as FunctionReference<'query', { secret: string }, string, 'internal'>,
  },
} as const;
