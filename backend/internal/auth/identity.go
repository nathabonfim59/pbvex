package auth

import (
	"context"
	"strings"

	"github.com/pocketbase/pocketbase/core"
)

type invocationContextKey struct{}

// InvocationMetadata is immutable request identity propagated through calls,
// realtime reruns, and nested runtime work.
type InvocationMetadata struct {
	Identity  *UserIdentity
	RequestID string
}

func WithInvocationMetadata(ctx context.Context, identity *UserIdentity, requestID string) context.Context {
	return context.WithValue(ctx, invocationContextKey{}, InvocationMetadata{Identity: identity, RequestID: requestID})
}

func InvocationMetadataFromContext(ctx context.Context) InvocationMetadata {
	metadata, _ := ctx.Value(invocationContextKey{}).(InvocationMetadata)
	return metadata
}

// UserIdentity is the stable, portable representation of an authenticated
// user inside the PBVex runtime. It deliberately mirrors the shape expected by
// Convex-compatible code so that user code can be moved between runtimes.
//
// The fields are derived from the PocketBase auth record and are not based on
// any client-supplied claims. Superusers do not receive a UserIdentity from
// ctx.auth.getUserIdentity(); they are not application users.
type UserIdentity struct {
	// Subject is a stable identifier for the end-user within the issuer.
	// For PocketBase records this is the record id.
	Subject string `json:"subject"`

	// TokenIdentifier is a globally unique string for this identity.
	// It combines the issuer and the subject so it is safe across multiple
	// auth collections.
	TokenIdentifier string `json:"tokenIdentifier"`

	// Issuer identifies the identity provider. For PocketBase records it is
	// the collection name qualified with a "pocketbase" namespace.
	Issuer string `json:"issuer"`

	// Standard OIDC profile claims. All optional fields are omitted from the
	// JSON representation when empty.
	Name                string `json:"name,omitempty"`
	GivenName           string `json:"givenName,omitempty"`
	FamilyName          string `json:"familyName,omitempty"`
	Nickname            string `json:"nickname,omitempty"`
	PreferredUsername   string `json:"preferredUsername,omitempty"`
	ProfileUrl          string `json:"profileUrl,omitempty"`
	PictureUrl          string `json:"pictureUrl,omitempty"`
	Email               string `json:"email,omitempty"`
	EmailVerified       bool   `json:"emailVerified,omitempty"`
	Gender              string `json:"gender,omitempty"`
	Birthday            string `json:"birthday,omitempty"`
	Timezone            string `json:"timezone,omitempty"`
	Language            string `json:"language,omitempty"`
	PhoneNumber         string `json:"phoneNumber,omitempty"`
	PhoneNumberVerified bool   `json:"phoneNumberVerified,omitempty"`
	Address             string `json:"address,omitempty"`
	UpdatedAt           string `json:"updatedAt,omitempty"`
}

// FromRecord maps a PocketBase auth record to a UserIdentity. It returns nil
// for nil records and for superusers, preventing superusers from gaining an
// application identity.
func FromRecord(record *core.Record) *UserIdentity {
	if record == nil || record.IsSuperuser() {
		return nil
	}

	collection := record.Collection()
	if collection == nil {
		return nil
	}

	// Collection names are mutable. Use PocketBase's stable collection id so a
	// rename cannot change an application's identity or recycle an old issuer.
	issuer := "pocketbase:" + collection.Id
	subject := record.Id
	tokenIdentifier := issuer + ":" + subject

	identity := &UserIdentity{
		Subject:         subject,
		TokenIdentifier: tokenIdentifier,
		Issuer:          issuer,
	}

	if email := record.Email(); email != "" {
		identity.Email = email
	}
	if username := record.GetString("username"); username != "" {
		identity.Nickname = username
		identity.PreferredUsername = username
	}

	if name, _ := record.Get("name").(string); name != "" {
		identity.Name = name
	}
	if givenName, _ := record.Get("firstName").(string); givenName != "" {
		identity.GivenName = givenName
	}
	if givenName, _ := record.Get("given_name").(string); givenName != "" {
		identity.GivenName = givenName
	}
	if familyName, _ := record.Get("lastName").(string); familyName != "" {
		identity.FamilyName = familyName
	}
	if familyName, _ := record.Get("family_name").(string); familyName != "" {
		identity.FamilyName = familyName
	}
	if profileUrl, _ := record.Get("profileUrl").(string); profileUrl != "" {
		identity.ProfileUrl = profileUrl
	}
	if profileUrl, _ := record.Get("profile").(string); profileUrl != "" {
		identity.ProfileUrl = profileUrl
	}
	if pictureUrl, _ := record.Get("pictureUrl").(string); pictureUrl != "" {
		identity.PictureUrl = pictureUrl
	}
	if pictureUrl, _ := record.Get("avatar").(string); pictureUrl != "" {
		identity.PictureUrl = pictureUrl
	}
	if verified := record.GetBool("verified"); verified {
		identity.EmailVerified = true
	}
	if gender, _ := record.Get("gender").(string); gender != "" {
		identity.Gender = gender
	}
	if birthday, _ := record.Get("birthdate").(string); birthday != "" {
		identity.Birthday = birthday
	}
	if timezone, _ := record.Get("timezone").(string); timezone != "" {
		identity.Timezone = timezone
	}
	if zoneinfo, _ := record.Get("zoneinfo").(string); zoneinfo != "" {
		identity.Timezone = zoneinfo
	}
	if language, _ := record.Get("language").(string); language != "" {
		identity.Language = language
	}
	if locale, _ := record.Get("locale").(string); locale != "" {
		identity.Language = locale
	}
	if phone, _ := record.Get("phone").(string); phone != "" {
		identity.PhoneNumber = phone
	}
	if phoneNumber, _ := record.Get("phone_number").(string); phoneNumber != "" {
		identity.PhoneNumber = phoneNumber
	}
	if phoneVerified, _ := record.Get("phone_verified").(bool); phoneVerified {
		identity.PhoneNumberVerified = true
	}
	if address, _ := record.Get("address").(string); address != "" {
		identity.Address = address
	}
	if updated := record.GetDateTime("updated"); !updated.IsZero() {
		identity.UpdatedAt = updated.Time().UTC().Format("2006-01-02T15:04:05Z")
	}

	return identity
}

// IdentityFromRequest returns the user identity for the authenticated record
// on a request event, or nil if the request is unauthenticated or the auth
// record is a superuser.
func IdentityFromRequest(e *core.RequestEvent) *UserIdentity {
	if e == nil {
		return nil
	}
	return FromRecord(e.Auth)
}

// ToMap converts a UserIdentity to a plain map so it can be exported to Goja.
// It includes only the non-zero optional fields.
func ToMap(identity *UserIdentity) map[string]any {
	if identity == nil {
		return nil
	}
	m := map[string]any{
		"subject":         identity.Subject,
		"tokenIdentifier": identity.TokenIdentifier,
		"issuer":          identity.Issuer,
	}
	if identity.Name != "" {
		m["name"] = identity.Name
	}
	if identity.GivenName != "" {
		m["givenName"] = identity.GivenName
	}
	if identity.FamilyName != "" {
		m["familyName"] = identity.FamilyName
	}
	if identity.Nickname != "" {
		m["nickname"] = identity.Nickname
	}
	if identity.PreferredUsername != "" {
		m["preferredUsername"] = identity.PreferredUsername
	}
	if identity.ProfileUrl != "" {
		m["profileUrl"] = identity.ProfileUrl
	}
	if identity.PictureUrl != "" {
		m["pictureUrl"] = identity.PictureUrl
	}
	if identity.Email != "" {
		m["email"] = identity.Email
	}
	if identity.EmailVerified {
		m["emailVerified"] = identity.EmailVerified
	}
	if identity.Gender != "" {
		m["gender"] = identity.Gender
	}
	if identity.Birthday != "" {
		m["birthday"] = identity.Birthday
	}
	if identity.Timezone != "" {
		m["timezone"] = identity.Timezone
	}
	if identity.Language != "" {
		m["language"] = identity.Language
	}
	if identity.PhoneNumber != "" {
		m["phoneNumber"] = identity.PhoneNumber
	}
	if identity.PhoneNumberVerified {
		m["phoneNumberVerified"] = identity.PhoneNumberVerified
	}
	if identity.Address != "" {
		m["address"] = identity.Address
	}
	if identity.UpdatedAt != "" {
		m["updatedAt"] = identity.UpdatedAt
	}
	return m
}

// IsSuperuserRequest returns true if the request event has a valid superuser
// auth record.
func IsSuperuserRequest(e *core.RequestEvent) bool {
	return e != nil && e.Auth != nil && e.Auth.IsSuperuser()
}

// SanitizedRequestID validates and normalizes an X-Request-Id header. If the
// value is missing or malformed, a new UUID is returned. Valid values are
// alphanumeric plus hyphen/underscore, between 1 and 64 characters.
func SanitizedRequestID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) == 0 || len(value) > 64 {
		return ""
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		if !isRequestIDChar(c) {
			return ""
		}
	}
	return value
}

func isRequestIDChar(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_'
}
