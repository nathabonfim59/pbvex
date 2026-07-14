import React, { type ReactNode } from 'react';
import type { Client } from '@pbvex/sdk-core';
export declare const PBVexContext: React.Context<Client | null>;
export interface PBVexProviderProps {
    client: Client;
    children?: ReactNode;
}
/**
 * Provides a PBVex Core Client to the component tree.
 *
 * Never closes the client on unmount; the client is owned by the caller.
 */
export declare function PBVexProvider({ client, children }: PBVexProviderProps): React.FunctionComponentElement<React.ProviderProps<Client | null>>;
//# sourceMappingURL=provider.d.ts.map