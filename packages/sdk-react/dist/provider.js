import React from 'react';
export const PBVexContext = React.createContext(null);
/**
 * Provides a PBVex Core Client to the component tree.
 *
 * Never closes the client on unmount; the client is owned by the caller.
 */
export function PBVexProvider({ client, children }) {
    return React.createElement(PBVexContext.Provider, { value: client }, children);
}
//# sourceMappingURL=provider.js.map