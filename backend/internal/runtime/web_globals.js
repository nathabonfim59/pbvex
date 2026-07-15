function normalizeBody(body) {
  if (body === undefined || body === null) return null;
  if (typeof body === 'string') return new TextEncoder().encode(body);
  if (body instanceof Uint8Array) return body;
  if (body instanceof ArrayBuffer) return new Uint8Array(body);
  return new TextEncoder().encode(String(body));
}

const headerStores = new WeakMap();

function headerStore(headers) {
  const store = headerStores.get(headers);
  if (!store) throw new TypeError('Illegal invocation');
  return store;
}

function normalizeHeaderPair(name, value) {
  name = String(name).toLowerCase();
  value = String(value);
  if (!__pbvexValidHeaderName(name)) throw new TypeError('Invalid HTTP header name');
  if (!__pbvexValidHeaderValue(value)) throw new TypeError('Invalid HTTP header value');
  return [name, value];
}

function assertHeaderBounds(store) {
  let count = 0;
  let totalBytes = 0;
  for (const name of Object.keys(store)) {
    for (const value of store[name]) {
      count++;
      totalBytes += name.length + __pbvexHeaderBytes(value);
    }
  }
  if (count > __pbvexHeaderLimits.count) throw new TypeError('HTTP headers exceed count limit');
  if (totalBytes > __pbvexHeaderLimits.totalBytes) throw new TypeError('HTTP headers exceed aggregate size limit');
}

class Headers {
  constructor(init) {
    headerStores.set(this, Object.create(null));
    if (init instanceof Headers) {
      init.forEach((value, name) => this.append(name, value));
    } else if (Array.isArray(init)) {
      for (const pair of init) {
        this.append(pair[0], pair[1]);
      }
    } else if (init && typeof init === 'object') {
      for (const name of Object.keys(init)) {
        this.set(name, init[name]);
      }
    }
  }

  append(name, value) {
    [name, value] = normalizeHeaderPair(name, value);
    const store = headerStore(this);
    if (!store[name]) store[name] = [];
    store[name].push(value);
    try { assertHeaderBounds(store); } catch (error) { store[name].pop(); if (!store[name].length) delete store[name]; throw error; }
  }

  set(name, value) {
    [name, value] = normalizeHeaderPair(name, value);
    const store = headerStore(this);
    const previous = store[name];
    store[name] = [value];
    try { assertHeaderBounds(store); } catch (error) { if (previous) store[name] = previous; else delete store[name]; throw error; }
  }

  get(name) {
    name = String(name).toLowerCase();
    const values = headerStore(this)[name];
    return values ? values[0] : null;
  }

  has(name) {
    return Object.prototype.hasOwnProperty.call(headerStore(this), String(name).toLowerCase());
  }

  delete(name) {
    delete headerStore(this)[String(name).toLowerCase()];
  }

  forEach(callback, thisArg) {
    const store = headerStore(this);
    for (const name of Object.keys(store)) {
      for (const value of store[name]) {
        callback.call(thisArg, value, name, this);
      }
    }
  }

  entries() {
    const out = [];
    const store = headerStore(this);
    for (const name of Object.keys(store)) {
      for (const value of store[name]) out.push([name, value]);
    }
    return out[Symbol.iterator]();
  }

  keys() {
    return Object.keys(headerStore(this))[Symbol.iterator]();
  }

  values() {
    const out = [];
    const store = headerStore(this);
    for (const name of Object.keys(store)) {
      out.push(...store[name]);
    }
    return out[Symbol.iterator]();
  }

  _export() {
    const out = Object.create(null);
    const store = headerStore(this);
    for (const name of Object.keys(store)) out[name] = store[name].slice();
    return out;
  }

  [Symbol.iterator]() {
    return this.entries();
  }
}

class Request {
  constructor(input, init) {
    init = init || {};
    this._url = String(input);
    this._method = init.method ? String(init.method).toUpperCase() : 'GET';
    this._headers = new Headers(init.headers);
    this._body = normalizeBody(init.body);
    if (this._method === 'GET' || this._method === 'HEAD') {
      this._body = null;
    }
    this._bodyUsed = false;
  }

  get method() { return this._method; }
  get url() { return this._url; }
  get headers() { return this._headers; }
  get body() { return this._body; }
  get bodyUsed() { return this._bodyUsed; }

  _consume() {
    if (this._bodyUsed) {
      throw new TypeError('Body has already been consumed');
    }
    this._bodyUsed = true;
    return this._body;
  }

  text() { return Promise.resolve(new TextDecoder().decode(this._consume())); }
  json() { return this.text().then((t) => JSON.parse(t)); }
  arrayBuffer() { var b = this._consume(); return Promise.resolve(b ? b.buffer : new ArrayBuffer(0)); }
}

class Response {
  constructor(body, init) {
    init = init || {};
    this._body = normalizeBody(body);
    this._status = init.status !== undefined ? init.status : 200;
    this._statusText = init.statusText !== undefined ? init.statusText : '';
    this._headers = new Headers(init.headers);
    this._type = 'default';
    this._url = '';
    this._bodyUsed = false;
    if (this._status === 204 || this._status === 205 || this._status === 304) {
      this._body = null;
    }
  }

  get status() { return this._status; }
  get statusText() { return this._statusText; }
  get headers() { return this._headers; }
  get body() { return this._body; }
  get bodyUsed() { return this._bodyUsed; }
  get ok() { return this._status >= 200 && this._status < 300; }
  get type() { return this._type; }
  get url() { return this._url; }

  _consume() {
    if (this._bodyUsed) {
      throw new TypeError('Body has already been consumed');
    }
    this._bodyUsed = true;
    return this._body;
  }

  text() { return Promise.resolve(new TextDecoder().decode(this._consume())); }
  json() { return this.text().then((t) => JSON.parse(t)); }
  arrayBuffer() { var b = this._consume(); return Promise.resolve(b ? b.buffer : new ArrayBuffer(0)); }
}

class TextEncoder {
  encode(s) { return __textEncoderEncode(s); }
}

class TextDecoder {
  constructor(label) { this.label = label || 'utf-8'; }
  decode(input) { return __textDecoderDecode(input); }
}

var url = require('url');

globalThis.Headers = Headers;
globalThis.Request = Request;
globalThis.Response = Response;
globalThis.TextEncoder = TextEncoder;
globalThis.TextDecoder = TextDecoder;
globalThis.URL = url.URL;
globalThis.URLSearchParams = url.URLSearchParams;
