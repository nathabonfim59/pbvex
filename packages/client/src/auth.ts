export type AuthRecord = Record<string, unknown> & {
  id: string;
  collectionId?: string;
  collectionName?: string;
  verified?: boolean;
};

export type AuthChangeListener<T extends AuthRecord = AuthRecord> = (
  token: string,
  record: T | null,
) => void;

function decodeJwtPayload(token: string): Record<string, unknown> {
  try {
    const part = token.split('.')[1];
    if (!part) return {};
    const normalized = part.replace(/-/g, '+').replace(/_/g, '/');
    const padded = normalized.padEnd(Math.ceil(normalized.length / 4) * 4, '=');
    if (typeof atob !== 'function') return {};
    const raw = atob(padded);
    const bytes = Uint8Array.from(raw, (char) => char.charCodeAt(0));
    const value = JSON.parse(new TextDecoder().decode(bytes));
    return value && typeof value === 'object' ? value : {};
  } catch {
    return {};
  }
}

export class AuthStore<T extends AuthRecord = AuthRecord> {
  protected currentToken = '';
  protected currentRecord: T | null = null;
  private readonly listeners = new Set<AuthChangeListener<T>>();

  get token(): string {
    return this.currentToken;
  }

  get record(): T | null {
    return this.currentRecord;
  }

  get isValid(): boolean {
    if (!this.token) return false;
    const exp = decodeJwtPayload(this.token).exp;
    return typeof exp === 'number' && exp > Date.now() / 1000;
  }

  get isSuperuser(): boolean {
    const payload = decodeJwtPayload(this.token);
    return payload.type === 'auth' && (
      this.record?.collectionName === '_superusers' ||
      (!this.record?.collectionName && payload.collectionId === 'pbc_3142635823')
    );
  }

  save(token: string, record?: T | null): void {
    this.currentToken = token || '';
    this.currentRecord = record ?? null;
    this.notify();
  }

  clear(): void {
    this.save('', null);
  }

  onChange(listener: AuthChangeListener<T>, fireImmediately = false): () => void {
    this.listeners.add(listener);
    if (fireImmediately) listener(this.token, this.record);
    return () => this.listeners.delete(listener);
  }

  protected notify(): void {
    for (const listener of this.listeners) listener(this.token, this.record);
  }
}

export interface AuthStorage {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
  removeItem(key: string): void;
}

export interface LocalAuthStoreOptions {
  key?: string;
  storage?: AuthStorage;
}

export class LocalAuthStore<T extends AuthRecord = AuthRecord> extends AuthStore<T> {
  private readonly key: string;
  private readonly storage?: AuthStorage;

  constructor(options: LocalAuthStoreOptions = {}) {
    super();
    this.key = options.key ?? 'pbvex_auth';
    this.storage = options.storage ?? safeLocalStorage();
    this.load();
    if (typeof window !== 'undefined' && this.storage === safeLocalStorage()) {
      window.addEventListener('storage', this.handleStorage);
    }
  }

  override save(token: string, record?: T | null): void {
    super.save(token, record);
    try {
      if (this.token) {
        this.storage?.setItem(this.key, JSON.stringify({ token: this.token, record: this.record }));
      } else {
        this.storage?.removeItem(this.key);
      }
    } catch {
      // Storage can be disabled or full; the in-memory session remains usable.
    }
  }

  private load(): void {
    try {
      const raw = this.storage?.getItem(this.key);
      if (!raw) return;
      const value = JSON.parse(raw) as { token?: unknown; record?: unknown };
      if (typeof value.token === 'string') {
        this.currentToken = value.token;
        this.currentRecord = value.record && typeof value.record === 'object'
          ? value.record as T
          : null;
      }
    } catch {
      // Ignore corrupt or unavailable storage.
    }
  }

  private readonly handleStorage = (event: StorageEvent): void => {
    if (event.key !== this.key) return;
    const previousToken = this.currentToken;
    const previousRecord = this.currentRecord;
    this.currentToken = '';
    this.currentRecord = null;
    this.load();
    if (previousToken !== this.currentToken || previousRecord !== this.currentRecord) this.notify();
  };
}

function safeLocalStorage(): AuthStorage | undefined {
  try {
    return typeof localStorage === 'undefined' ? undefined : localStorage;
  } catch {
    return undefined;
  }
}

export interface AuthProviderInfo {
  name: string;
  displayName: string;
  state: string;
  authURL: string;
  codeVerifier: string;
  codeChallenge: string;
  codeChallengeMethod: string;
}

export interface AuthMethodsList {
  mfa: { enabled: boolean; duration: number };
  otp: { enabled: boolean; duration: number };
  password: { enabled: boolean; identityFields: string[] };
  oauth2: { enabled: boolean; providers: AuthProviderInfo[] };
}

export interface AuthResponse<T extends AuthRecord = AuthRecord> {
  token: string;
  record: T;
  meta?: Record<string, unknown>;
}

export interface OtpResponse {
  otpId: string;
}

export interface AuthRequestOptions {
  signal?: AbortSignal;
  /** PocketBase MFA session id returned by the first successful auth method. */
  mfaId?: string;
  query?: Record<string, string | number | boolean | undefined>;
  headers?: Record<string, string>;
  body?: Record<string, unknown>;
}

export interface OAuth2Options extends AuthRequestOptions {
  provider: string;
  scopes?: string[];
  createData?: Record<string, unknown>;
  urlCallback?: (url: string) => void | Promise<void>;
}

export interface PocketBaseErrorBody {
  status?: number;
  message?: string;
  data?: Record<string, unknown>;
  [key: string]: unknown;
}

export class AuthApiError extends Error {
  readonly status: number;
  readonly response: PocketBaseErrorBody;
  readonly url: string;

  constructor(status: number, response: PocketBaseErrorBody, url: string) {
    super(response.message || `PocketBase auth request failed with status ${status}`);
    this.name = 'AuthApiError';
    this.status = status;
    this.response = response;
    this.url = url;
  }
}

export type AuthRequest = <T>(
  path: string,
  init?: { method?: string; body?: Record<string, unknown>; options?: AuthRequestOptions; auth?: boolean },
) => Promise<T>;

export class AuthCollection<T extends AuthRecord = AuthRecord> {
  constructor(
    private readonly name: string,
    private readonly request: AuthRequest,
    private readonly store: AuthStore<T>,
    private readonly oauth: (collection: string, options: OAuth2Options) => Promise<AuthResponse<T>>,
  ) {}

  private get path(): string {
    return `/api/collections/${encodeURIComponent(this.name)}`;
  }

  private save(response: AuthResponse<T>): AuthResponse<T> {
    this.store.save(response.token, response.record);
    return response;
  }

  listAuthMethods(options?: AuthRequestOptions): Promise<AuthMethodsList> {
    return this.request(`${this.path}/auth-methods`, { options });
  }

  async authWithPassword(identity: string, password: string, options?: AuthRequestOptions): Promise<AuthResponse<T>> {
    const response = await this.request<AuthResponse<T>>(`${this.path}/auth-with-password`, {
      method: 'POST', body: { identity, password, ...(options?.mfaId ? { mfaId: options.mfaId } : {}), ...options?.body }, options,
    });
    return this.save(response);
  }

  requestOTP(email: string, options?: AuthRequestOptions): Promise<OtpResponse> {
    return this.request(`${this.path}/request-otp`, {
      method: 'POST', body: { email, ...options?.body }, options,
    });
  }

  async authWithOTP(otpId: string, password: string, options?: AuthRequestOptions): Promise<AuthResponse<T>> {
    const response = await this.request<AuthResponse<T>>(`${this.path}/auth-with-otp`, {
      method: 'POST', body: { otpId, password, ...(options?.mfaId ? { mfaId: options.mfaId } : {}), ...options?.body }, options,
    });
    return this.save(response);
  }

  authWithOAuth2(options: OAuth2Options): Promise<AuthResponse<T>> {
    return this.oauth(this.name, options);
  }

  async authWithOAuth2Code(
    provider: string,
    code: string,
    codeVerifier: string,
    redirectURL: string,
    createData?: Record<string, unknown>,
    options?: AuthRequestOptions,
  ): Promise<AuthResponse<T>> {
    const response = await this.request<AuthResponse<T>>(`${this.path}/auth-with-oauth2`, {
      method: 'POST',
      body: { provider, code, codeVerifier, redirectURL, createData, ...(options?.mfaId ? { mfaId: options.mfaId } : {}), ...options?.body },
      options,
    });
    return this.save(response);
  }

  async authRefresh(options?: AuthRequestOptions): Promise<AuthResponse<T>> {
    const response = await this.request<AuthResponse<T>>(`${this.path}/auth-refresh`, {
      method: 'POST', options, auth: true,
    });
    return this.save(response);
  }

  async requestPasswordReset(email: string, options?: AuthRequestOptions): Promise<true> {
    await this.request(`${this.path}/request-password-reset`, { method: 'POST', body: { email, ...options?.body }, options });
    return true;
  }

  async confirmPasswordReset(token: string, password: string, passwordConfirm: string, options?: AuthRequestOptions): Promise<true> {
    await this.request(`${this.path}/confirm-password-reset`, {
      method: 'POST', body: { token, password, passwordConfirm, ...options?.body }, options,
    });
    return true;
  }

  async requestVerification(email: string, options?: AuthRequestOptions): Promise<true> {
    await this.request(`${this.path}/request-verification`, { method: 'POST', body: { email, ...options?.body }, options });
    return true;
  }

  async confirmVerification(token: string, options?: AuthRequestOptions): Promise<true> {
    await this.request(`${this.path}/confirm-verification`, { method: 'POST', body: { token, ...options?.body }, options });
    const payload = decodeJwtPayload(token);
    const record = this.store.record;
    if (record !== null && record.id === payload.id && record.collectionId === payload.collectionId) {
      this.store.save(this.store.token, { ...record, verified: true } as T);
    }
    return true;
  }

  async requestEmailChange(newEmail: string, options?: AuthRequestOptions): Promise<true> {
    await this.request(`${this.path}/request-email-change`, {
      method: 'POST', body: { newEmail, ...options?.body }, options, auth: true,
    });
    return true;
  }

  async confirmEmailChange(token: string, password: string, options?: AuthRequestOptions): Promise<true> {
    await this.request(`${this.path}/confirm-email-change`, {
      method: 'POST', body: { token, password, ...options?.body }, options,
    });
    const payload = decodeJwtPayload(token);
    const record = this.store.record;
    if (record !== null && record.id === payload.id && record.collectionId === payload.collectionId) {
      this.store.clear();
    }
    return true;
  }

  /** Returns impersonated auth data without replacing the current (usually superuser) store. */
  impersonate(recordId: string, duration = 0, options?: AuthRequestOptions): Promise<AuthResponse<T>> {
    return this.request(`${this.path}/impersonate/${encodeURIComponent(recordId)}`, {
      method: 'POST', body: { duration, ...options?.body }, options, auth: true,
    });
  }
}

export class AuthClient {
  private readonly collections = new Map<string, AuthCollection<any>>();

  constructor(
    private readonly request: AuthRequest,
    private readonly store: AuthStore,
    private readonly oauth: (collection: string, options: OAuth2Options) => Promise<AuthResponse>,
  ) {}

  collection<T extends AuthRecord = AuthRecord>(name = 'users'): AuthCollection<T> {
    let service = this.collections.get(name);
    if (!service) {
      service = new AuthCollection(name, this.request, this.store, this.oauth);
      this.collections.set(name, service);
    }
    return service as AuthCollection<T>;
  }
}
