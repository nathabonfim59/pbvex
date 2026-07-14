import { defineComponentFns } from 'pbvex/server';
import { kid } from './component.js';

const functions = defineComponentFns(kid);
export const kidFn = functions.query({ handler: async () => 'child' });
