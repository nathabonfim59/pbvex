import type { ActionCtx, GenericDataModel, HttpActionCtx, MutationCtx, QueryCtx } from '../src/runtime/server.js';

declare const action: ActionCtx<GenericDataModel, 'receipt' | 'welcome'>;
void action.email.send({ template: 'welcome', to: 'person@example.com', variables: { name: 'Pat' } });
void action.email.send({ template: 'receipt', to: ['person@example.com'], cc: 'copy@example.com' });
// @ts-expect-error template names outside the generated catalog are rejected
void action.email.send({ template: 'unknown', to: 'person@example.com' });

declare const httpAction: HttpActionCtx<GenericDataModel, 'welcome'>;
void httpAction.email.send({ template: 'welcome', to: 'person@example.com' });

declare const query: QueryCtx;
// @ts-expect-error queries cannot deliver email
void query.email;

declare const mutation: MutationCtx;
// @ts-expect-error mutations cannot deliver email
void mutation.email;
