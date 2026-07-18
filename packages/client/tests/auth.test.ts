import { describe, expect, it, vi } from 'vitest';
import {
  AuthApiError,
  AuthStore,
  Client,
  LocalAuthStore,
  type AuthStorage,
} from '../src/index.js';
import type { MockedFunction } from 'vitest';

function makeMockFetch(handler: (request: Request) => Response | Promise<Response>): MockedFunction<typeof fetch> {
  return vi.fn((input: RequestInfo | URL, init?: RequestInit) =>
    Promise.resolve(handler(new Request(input, init)))) as unknown as MockedFunction<typeof fetch>;
}

function json(value: unknown, status = 200): Response {
  return new Response(JSON.stringify(value), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function token(exp = Math.floor(Date.now() / 1000) + 3600): string {
  return `e30.${btoa(JSON.stringify({ type: 'auth', exp, collectionId: 'users-id' }))}.sig`;
}

describe('native PocketBase authentication', () => {
  it('uses the browser global as the fetch receiver', async () => {
    const browserFetch = function (this: unknown) {
      expect(this).toBe(globalThis);
      return Promise.resolve(json({ token: token(), record: { id: 'user-1' } }));
    } as typeof fetch;
    const client = new Client('http://localhost:8090', { fetch: browserFetch });

    await expect(client.auth.collection('users').authWithPassword('person@example.com', 'secret'))
      .resolves.toMatchObject({ record: { id: 'user-1' } });
  });

  it('creates an auth record without changing the auth store', async () => {
    const fetch = makeMockFetch(async (request) => {
      expect(request.url).toBe('http://localhost:8090/api/collections/users/records');
      expect(request.method).toBe('POST');
      expect(await request.json()).toEqual({
        email: 'person@example.com',
        password: 'correct horse battery staple',
        passwordConfirm: 'correct horse battery staple',
        name: 'Person',
      });
      return json({ id: 'user-1', collectionId: 'users-id', name: 'Person' });
    });
    const store = new AuthStore();
    const client = new Client('http://localhost:8090', { fetch, authStore: store });

    const record = await client.auth.collection('users').create({
      email: 'person@example.com',
      password: 'correct horse battery staple',
      passwordConfirm: 'correct horse battery staple',
      name: 'Person',
    });

    expect(record).toMatchObject({ id: 'user-1', name: 'Person' });
    expect(store.isValid).toBe(false);
  });

  it('authenticates with password, saves the record, and authenticates PBVex calls', async () => {
    const authToken = token();
    const fetch = makeMockFetch(async (request) => {
      if (request.url.endsWith('/api/collections/users/auth-with-password')) {
        expect(request.method).toBe('POST');
        expect(await request.json()).toEqual({
          identity: 'person@example.com',
          password: 'secret',
        });
        return json({ token: authToken, record: { id: 'user-1', collectionId: 'users-id' } });
      }
      expect(request.url).toBe('http://localhost:8090/api/pbvex/call');
      expect(request.headers.get('authorization')).toBe(`Bearer ${authToken}`);
      return json({ result: 'ok' });
    });
    const client = new Client('http://localhost:8090', { fetch, authStore: new AuthStore() });

    const auth = await client.auth.collection('users').authWithPassword('person@example.com', 'secret');
    expect(auth.record.id).toBe('user-1');
    expect(client.authStore.token).toBe(authToken);
    expect(client.authStore.isValid).toBe(true);
    await expect(client.query('private:hello')).resolves.toBe('ok');
  });

  it('passes MFA data through request bodies and exposes PocketBase error fields', async () => {
    const fetch = makeMockFetch(async (request) => {
      const body = await request.json() as Record<string, unknown>;
      if (request.url.endsWith('/auth-with-password')) {
        return json({ status: 401, message: 'MFA required', mfaId: 'mfa-1' }, 401);
      }
      expect(body).toEqual({ otpId: 'otp-1', password: '123456', mfaId: 'mfa-1' });
      return json({ token: token(), record: { id: 'user-1' } });
    });
    const client = new Client('http://localhost:8090', { fetch, authStore: new AuthStore() });
    const users = client.auth.collection('users');

    await expect(users.authWithPassword('person@example.com', 'secret')).rejects.toMatchObject({
      status: 401,
      response: { mfaId: 'mfa-1' },
    });
    try {
      await users.authWithPassword('person@example.com', 'secret');
    } catch (error) {
      expect(error).toBeInstanceOf(AuthApiError);
    }
    await users.authWithOTP('otp-1', '123456', { mfaId: 'mfa-1' });
  });

  it('uses bearer auth for refresh and email-change requests', async () => {
    const oldToken = token();
    const newToken = token(Math.floor(Date.now() / 1000) + 7200);
    const store = new AuthStore();
    store.save(oldToken, { id: 'user-1' });
    const fetch = makeMockFetch((request) => {
      expect(request.headers.get('authorization')).toBe(`Bearer ${oldToken}`);
      if (request.url.endsWith('/auth-refresh')) {
        return json({ token: newToken, record: { id: 'user-1' } });
      }
      return new Response(null, { status: 204 });
    });
    const client = new Client('http://localhost:8090', { fetch, authStore: store });
    const users = client.auth.collection('users');

    await users.authRefresh();
    expect(store.token).toBe(newToken);
    // The refreshed token is used by subsequent authenticated account operations.
    fetch.mockImplementation((input: RequestInfo | URL, init?: RequestInit) => {
      const request = new Request(input, init);
      expect(request.headers.get('authorization')).toBe(`Bearer ${newToken}`);
      return Promise.resolve(new Response(null, { status: 204 }));
    });
    await expect(users.requestEmailChange('new@example.com')).resolves.toBe(true);
  });

  it('persists local auth state and clears it', () => {
    const values = new Map<string, string>();
    const storage: AuthStorage = {
      getItem: (key) => values.get(key) ?? null,
      setItem: (key, value) => { values.set(key, value); },
      removeItem: (key) => { values.delete(key); },
    };
    const first = new LocalAuthStore({ storage, key: 'test-auth' });
    first.save(token(), { id: 'user-1' });

    const restored = new LocalAuthStore({ storage, key: 'test-auth' });
    expect(restored.record?.id).toBe('user-1');
    expect(restored.isValid).toBe(true);
    restored.clear();
    expect(values.has('test-auth')).toBe(false);
  });

  it('supports OTP and manual OAuth2 code exchange routes', async () => {
    const fetch = makeMockFetch(async (request) => {
      if (request.url.endsWith('/request-otp')) {
        expect(await request.json()).toEqual({ email: 'person@example.com' });
        return json({ otpId: 'otp-1' });
      }
      expect(request.url.endsWith('/auth-with-oauth2')).toBe(true);
      expect(await request.json()).toEqual({
        provider: 'google',
        code: 'code',
        codeVerifier: 'verifier',
        redirectURL: 'myapp://callback',
        createData: { plan: 'pro' },
      });
      return json({ token: token(), record: { id: 'user-1' } });
    });
    const client = new Client('http://localhost:8090', { fetch, authStore: new AuthStore() });
    const users = client.auth.collection('users');

    await expect(users.requestOTP('person@example.com')).resolves.toEqual({ otpId: 'otp-1' });
    await expect(users.authWithOAuth2Code(
      'google', 'code', 'verifier', 'myapp://callback', { plan: 'pro' },
    )).resolves.toMatchObject({ record: { id: 'user-1' } });
  });
});
