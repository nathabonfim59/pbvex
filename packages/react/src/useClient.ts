import { useContext } from 'react';
import type { Client } from '@pbvex/client';
import { PBVexContext } from './provider.js';

export function usePBVexClient(): Client {
  const client = useContext(PBVexContext);
  if (!client) {
    throw new Error('usePBVexClient must be used within a PBVexProvider');
  }
  return client;
}
