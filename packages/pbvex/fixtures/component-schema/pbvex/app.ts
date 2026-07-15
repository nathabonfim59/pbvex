import { defineApp, mount } from 'pbvex/server';
import { counter } from './component';

export default defineApp({ components: [mount(counter, 'counter'), mount(counter, 'counter2')] });
