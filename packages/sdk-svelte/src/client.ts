import { getContext, setContext } from 'svelte';
import { Client } from '@pbvex/sdk-core';

const CLIENT_CONTEXT_KEY = Symbol('pbvex-client');

export function setClient(client: Client): Client {
  setContext(CLIENT_CONTEXT_KEY, client);
  return client;
}

export function getClient(): Client {
  const client = getContext<Client | undefined>(CLIENT_CONTEXT_KEY);
  if (!client) {
    throw new Error(
      'No PBVex client found in Svelte context. Call setClient(client) in a parent component or pass a client explicitly.',
    );
  }
  return client;
}
