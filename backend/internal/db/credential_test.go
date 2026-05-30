package db

import "testing"

func TestCredentialEncryptionRoundTrip(t *testing.T) {
	key, err := deriveCredentialKey("local-test-secret")
	if err != nil {
		t.Fatal(err)
	}
	store := &Store{credentialKey: key}

	encrypted, err := store.encryptAPIKey("sk-test")
	if err != nil {
		t.Fatal(err)
	}
	if encrypted == "" || encrypted == "sk-test" {
		t.Fatalf("api key was not encrypted: %q", encrypted)
	}
	decrypted, err := store.decryptAPIKey(encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if decrypted != "sk-test" {
		t.Fatalf("unexpected decrypted key: %q", decrypted)
	}
}

func TestCredentialKeyRequiresSecret(t *testing.T) {
	if _, err := deriveCredentialKey(" "); err == nil {
		t.Fatal("expected empty credential key to fail")
	}
}

func TestAPIKeyHashIsStableAndTrimmed(t *testing.T) {
	first := apiKeyHash(" sk-test ")
	second := apiKeyHash("sk-test")
	if first == "" || first != second {
		t.Fatalf("unexpected hash values: %q %q", first, second)
	}
	if first == "sk-test" {
		t.Fatal("hash should not equal the raw api key")
	}
}
