package storage

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nathabonfim59/pbvex/backend/internal/schema"
	"github.com/pocketbase/dbx"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/types"
)

const keyringPurposeURLSigning = "url-signing"

// keyring manages persisted, versioned signing keys for short-lived storage URLs.
type keyring struct {
	app    core.App
	config Config

	mu      sync.RWMutex
	current keyVersion
	keys    map[string]keyVersion
	loaded  bool
}

const signingKeyBytes = 32

type keyVersion struct {
	id        string
	key       []byte
	createdAt time.Time
	expiresAt time.Time
}

func newKeyring(app core.App, config Config) *keyring {
	return &keyring{
		app:    app,
		config: config,
		keys:   make(map[string]keyVersion),
	}
}

// LoadOrCreate ensures the keyring is loaded from disk and that a usable current key exists.
func (k *keyring) LoadOrCreate(ctx context.Context) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	if k.loaded {
		if k.current.isValid() {
			return nil
		}
	}

	records, err := k.loadRecords(ctx)
	if err != nil {
		return err
	}

	k.keys = make(map[string]keyVersion)
	var latest keyVersion
	for _, rec := range records {
		kv, err := recordToKeyVersion(rec)
		if err != nil {
			k.app.Logger().Error("ignoring corrupt storage signing key", "keyId", rec.GetString(schema.FieldKeyringKeyID), "error", err)
			continue
		}
		if kv.expiresAt.Before(time.Now().UTC()) {
			// Stale key; schedule for cleanup by skipping it.
			continue
		}
		k.keys[kv.id] = kv
		if kv.createdAt.After(latest.createdAt) && kv.createdAt.Add(k.config.KeyRotationInterval).After(time.Now().UTC()) {
			latest = kv
		}
	}

	if latest.id == "" {
		kv, err := k.generate(ctx)
		if err != nil {
			return err
		}
		k.keys[kv.id] = kv
		latest = kv
	}

	k.current = latest
	k.loaded = true
	return nil
}

// Current returns the signing key to use for new URLs. It transparently rotates
// the current key if its active rotation interval has passed.
func (k *keyring) Current(ctx context.Context) (keyVersion, error) {
	if err := k.LoadOrCreate(ctx); err != nil {
		return keyVersion{}, err
	}

	k.mu.RLock()
	cur := k.current
	k.mu.RUnlock()

	if cur.isValid() && cur.createdAt.Add(k.config.KeyRotationInterval).After(time.Now().UTC()) {
		return cur, nil
	}

	k.mu.Lock()
	defer k.mu.Unlock()

	// Double-check after acquiring write lock.
	if k.current.isValid() && k.current.createdAt.Add(k.config.KeyRotationInterval).After(time.Now().UTC()) {
		return k.current, nil
	}

	newKey, err := k.generate(ctx)
	if err != nil {
		return keyVersion{}, err
	}
	k.keys[newKey.id] = newKey
	k.current = newKey
	return newKey, nil
}

// CurrentCommitted returns an already-persisted signing key without writing to
// the database. It is used from user mutation transactions, where rotating via
// the root app would contend with the transaction's SQLite writer lock.
func (k *keyring) CurrentCommitted(ttl time.Duration) (keyVersion, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()

	if !k.loaded || !k.current.isValid() {
		return keyVersion{}, fmt.Errorf("no committed storage signing key available")
	}
	if !k.current.expiresAt.After(time.Now().UTC().Add(ttl)) {
		return keyVersion{}, fmt.Errorf("committed storage signing key expires before signed url")
	}
	return k.current, nil
}

// Get returns a key by id. It is used for verification and tolerates keys that
// are still within their grace period.
func (k *keyring) Get(ctx context.Context, id string) (keyVersion, error) {
	if err := k.LoadOrCreate(ctx); err != nil {
		return keyVersion{}, err
	}

	k.mu.RLock()
	defer k.mu.RUnlock()

	kv, ok := k.keys[id]
	if !ok {
		return keyVersion{}, fmt.Errorf("key %s not found", id)
	}
	if !kv.isValid() {
		return keyVersion{}, fmt.Errorf("key %s expired", id)
	}
	return kv, nil
}

// Prune removes keys whose retention has expired.
func (k *keyring) Prune(ctx context.Context) (int64, error) {
	records := []*core.Record{}
	err := k.app.RecordQuery(schema.CollectionStorageKeyring).
		AndWhere(dbx.NewExp(fmt.Sprintf("%s < {:now}", schema.FieldKeyringExpiresAt), dbx.Params{"now": types.NowDateTime()})).
		All(&records)
	if err != nil {
		return 0, err
	}
	for _, rec := range records {
		if err := k.app.DeleteWithContext(schema.WithInternalContext(ctx), rec); err != nil {
			return 0, err
		}
	}

	k.mu.Lock()
	for id, kv := range k.keys {
		if kv.expiresAt.Before(time.Now().UTC()) {
			delete(k.keys, id)
		}
	}
	if k.current.expiresAt.Before(time.Now().UTC()) {
		k.current = keyVersion{}
	}
	k.mu.Unlock()

	return int64(len(records)), nil
}

func (k *keyring) loadRecords(ctx context.Context) ([]*core.Record, error) {
	records := []*core.Record{}
	err := k.app.RecordQuery(schema.CollectionStorageKeyring).
		AndWhere(dbx.NewExp(fmt.Sprintf("%s = {:purpose}", schema.FieldKeyringPurpose), dbx.Params{"purpose": keyringPurposeURLSigning})).
		All(&records)
	if err != nil {
		return nil, err
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].GetDateTime(schema.FieldKeyringCreatedAt).Time().After(records[j].GetDateTime(schema.FieldKeyringCreatedAt).Time())
	})
	return records, nil
}

func (k *keyring) generate(ctx context.Context) (keyVersion, error) {
	buf := make([]byte, signingKeyBytes)
	if _, err := rand.Read(buf); err != nil {
		return keyVersion{}, fmt.Errorf("failed to generate signing key: %w", err)
	}

	id, err := GenerateAttempt()
	if err != nil {
		return keyVersion{}, fmt.Errorf("failed to generate key id: %w", err)
	}

	now := time.Now().UTC()
	kv := keyVersion{
		id:        id,
		key:       buf,
		createdAt: now,
		expiresAt: now.Add(k.config.KeyRotationInterval + k.config.KeyGracePeriod),
	}

	col, err := k.app.FindCollectionByNameOrId(schema.CollectionStorageKeyring)
	if err != nil {
		return keyVersion{}, fmt.Errorf("failed to find keyring collection: %w", err)
	}
	rec := core.NewRecord(col)
	rec.Set(schema.FieldKeyringKeyID, kv.id)
	rec.Set(schema.FieldKeyringKey, base64.StdEncoding.EncodeToString(kv.key))
	rec.Set(schema.FieldKeyringPurpose, keyringPurposeURLSigning)
	rec.Set(schema.FieldKeyringCreatedAt, types.NowDateTime())
	expiresDt, err := types.ParseDateTime(kv.expiresAt)
	if err != nil {
		return keyVersion{}, fmt.Errorf("failed to parse keyring expiration: %w", err)
	}
	rec.Set(schema.FieldKeyringExpiresAt, expiresDt)

	if err := k.app.SaveWithContext(schema.WithInternalContext(ctx), rec); err != nil {
		return keyVersion{}, fmt.Errorf("failed to persist signing key: %w", err)
	}
	return kv, nil
}

func recordToKeyVersion(rec *core.Record) (keyVersion, error) {
	k := rec.GetString(schema.FieldKeyringKey)
	key, err := base64.StdEncoding.Strict().DecodeString(k)
	if err != nil {
		return keyVersion{}, fmt.Errorf("invalid signing key encoding: %w", err)
	}
	if len(key) != signingKeyBytes {
		return keyVersion{}, fmt.Errorf("invalid signing key length: got %d, want %d", len(key), signingKeyBytes)
	}
	kv := keyVersion{
		id:        rec.GetString(schema.FieldKeyringKeyID),
		key:       key,
		createdAt: rec.GetDateTime(schema.FieldKeyringCreatedAt).Time(),
		expiresAt: rec.GetDateTime(schema.FieldKeyringExpiresAt).Time(),
	}
	if kv.id == "" || kv.createdAt.IsZero() || kv.expiresAt.IsZero() {
		return keyVersion{}, fmt.Errorf("invalid signing key metadata")
	}
	return kv, nil
}

func (kv keyVersion) isValid() bool {
	return kv.id != "" && len(kv.key) > 0 && kv.expiresAt.After(time.Now().UTC())
}

func (kv keyVersion) sign(payload string) []byte {
	mac := hmac.New(sha256.New, kv.key)
	mac.Write([]byte(payload))
	return mac.Sum(nil)
}

func (kv keyVersion) verify(payload string, sig []byte) bool {
	mac := hmac.New(sha256.New, kv.key)
	mac.Write([]byte(payload))
	return hmac.Equal(mac.Sum(nil), sig)
}

// signURL signs a download URL with the current key and returns the full signed URL.
func (s *Service) signURL(ctx context.Context, storageID string, auth AuthContext, ttl time.Duration, capability bool) (string, error) {
	if ttl <= 0 {
		ttl = s.config.URLSigningTTL
	}
	if ttl > s.config.URLSigningMaxTTL {
		return "", fmt.Errorf("requested url ttl exceeds maximum")
	}

	var key keyVersion
	var err error
	if app := s.appFor(ctx); app != nil && app.IsTransactional() {
		key, err = s.kr.CurrentCommitted(ttl)
	} else {
		key, err = s.kr.Current(schema.WithInternalContext(ctx))
	}
	if err != nil {
		return "", err
	}

	base := s.config.BaseURL
	if base == "" {
		base = s.app.Settings().Meta.AppURL
	}
	base = strings.TrimRight(base, "/")
	path := s.config.BasePath + "/" + url.PathEscape(storageID)

	nonce, err := GenerateAttempt()
	if err != nil {
		return "", err
	}

	exp := time.Now().UTC().Add(ttl).Unix()
	v := url.Values{}
	v.Set("v", key.id)
	v.Set("pid", storageID)
	v.Set("exp", fmt.Sprintf("%d", exp))
	v.Set("sub", auth.identifier())
	v.Set("pol", "download")
	v.Set("nonce", nonce)
	if capability {
		v.Set("bnd", "capability")
	}

	payload := path + "?" + v.Encode()
	sig := base64.RawURLEncoding.EncodeToString(key.sign(payload))
	v.Set("sig", sig)

	if base == "" {
		return path + "?" + v.Encode(), nil
	}
	return base + path + "?" + v.Encode(), nil
}

// verifySignedURL validates the signature and constraints of a signed download URL
// against the identity of the caller. Legacy/default URLs require the "sub"
// claim to match the caller. URLs signed with bnd=capability are bearer URLs
// and require an empty subject. The "pol" claim must be "download".
func (s *Service) verifySignedURL(storageID string, u *url.URL, auth AuthContext) error {
	q := u.Query()
	sig := q.Get("sig")
	q.Del("sig")
	// The thumb selector is not an authorization claim. The serving path checks
	// it against the immutable image policy persisted with the file.
	q.Del("thumb")
	if sig == "" {
		return ErrURLTampered
	}

	payload := u.Path + "?" + q.Encode()
	keyID := q.Get("v")
	if keyID == "" {
		return ErrURLTampered
	}

	ctx := schema.WithInternalContext(context.Background())
	key, err := s.kr.Get(ctx, keyID)
	if err != nil {
		return ErrURLTampered
	}

	sigBytes, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return ErrURLTampered
	}
	if !key.verify(payload, sigBytes) {
		return ErrURLTampered
	}

	if q.Get("pid") != storageID {
		return ErrURLTampered
	}
	if q.Get("pol") != "download" {
		return ErrURLTampered
	}
	switch q.Get("bnd") {
	case "":
		if q.Get("sub") != auth.identifier() {
			return ErrURLForbidden
		}
	case "capability":
		if q.Get("sub") != "" {
			return ErrURLTampered
		}
	default:
		return ErrURLTampered
	}
	exp, err := parseInt(q.Get("exp"))
	if err != nil {
		return ErrURLTampered
	}
	if time.Now().UTC().Unix() >= exp {
		return ErrURLExpired
	}
	return nil
}

func parseInt(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}
