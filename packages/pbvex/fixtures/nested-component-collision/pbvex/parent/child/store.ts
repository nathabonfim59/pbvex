import { defineComponentFns } from 'pbvex/server';
import { parent } from '../component.js';

const functions = defineComponentFns(parent);
export const parentNested = functions.query({ handler: async () => 'parent' });
