import { defineApp, mount } from 'pbvex/server';
import { kid } from './kid/component.js';
import { parent } from './parent/component.js';

export default defineApp({
  components: [mount(parent, 'outer', { children: [mount(kid, 'child')] })],
});
