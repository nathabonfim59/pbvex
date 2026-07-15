import React, { type ReactNode } from 'react';
import type { Client } from '@pbvex/client';

export const PBVexContext = React.createContext<Client | null>(null);

export interface PBVexProviderProps {
  client: Client;
  children?: ReactNode;
}

/**
 * Provides a PBVex Core Client to the component tree.
 *
 * Never closes the client on unmount; the client is owned by the caller.
 */
export function PBVexProvider({ client, children }: PBVexProviderProps) {
  return React.createElement(PBVexContext.Provider, { value: client }, children);
}
