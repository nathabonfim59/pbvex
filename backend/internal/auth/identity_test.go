package auth

import (
	"testing"

	"github.com/pocketbase/pocketbase/core"
)

func TestTokenIdentifierUsesStableCollectionIdentity(t *testing.T) {
	first := core.NewAuthCollection("members")
	second := core.NewAuthCollection("customers")
	first.Id = "collection_one"
	second.Id = "collection_two"

	firstRecord := core.NewRecord(first)
	firstRecord.Id = "same_record_id"
	secondRecord := core.NewRecord(second)
	secondRecord.Id = "same_record_id"

	firstIdentity := FromRecord(firstRecord)
	secondIdentity := FromRecord(secondRecord)
	if firstIdentity == nil || secondIdentity == nil {
		t.Fatal("auth records must produce identities")
	}
	if firstIdentity.TokenIdentifier == secondIdentity.TokenIdentifier {
		t.Fatalf("same record id in different auth collections collided: %q", firstIdentity.TokenIdentifier)
	}
	if firstIdentity.TokenIdentifier != "pocketbase:collection_one:same_record_id" {
		t.Fatalf("token identifier %q", firstIdentity.TokenIdentifier)
	}

	before := *firstIdentity
	first.Name = "renamed_members"
	after := FromRecord(firstRecord)
	if after == nil || after.Issuer != before.Issuer || after.TokenIdentifier != before.TokenIdentifier {
		t.Fatalf("collection rename changed identity: before=%#v after=%#v", before, after)
	}
}
