import { Client, Action, action } from '@pbvex/client';

// Desired action definition and call site.

export const sendEmail = action('sendEmail', async (to: string, subject: string) => {
  return { delivered: true };
});

const client = new Client();

async function callSite(): Promise<void> {
  const result = await client.action('sendEmail', 'user@example.com', 'Hello');
  const direct = await sendEmail.run('user@example.com', 'Hello');
  const viaAction = await new Action('sendEmail').run('user@example.com', 'Hello');
  console.log(result, direct, viaAction);
}

export { callSite };
